package timeline

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/melqtx/xeet/pkg/api"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const maxPreviewBytes = 10 << 20

const (
	// prefetchBehind/prefetchAhead bound how many neighbours of the selected
	// post download eagerly so scrolling lands on already-cached images.
	prefetchBehind = 2
	prefetchAhead  = 4
	// inlineImageRadius bounds how many posts around the selection render
	// their cached image inline; further posts show a media chip instead,
	// which keeps each frame's render cost flat.
	inlineImageRadius = 6
	// maxCachedPreviews bounds memory and temp-file usage; entries further
	// than previewKeepRadius posts from the selection are eligible to evict.
	maxCachedPreviews = 48
	previewKeepRadius = 16
)

type imageMode string

const (
	imageModeNative  imageMode = "native"
	imageModeWezTerm imageMode = "iterm2"
	imageModeANSI    imageMode = "ansi"
	imageModeOff     imageMode = "off"
)

// resolveImageMode picks the renderer and explains any downgrade from the
// terminal's native graphics so the help overlay can show why images look
// the way they do.
func resolveImageMode(requested string) (imageMode, string) {
	if requested == "off" {
		return imageModeOff, ""
	}
	if requested == "ansi" {
		return imageModeANSI, ""
	}
	// Zellij does not currently pass the Kitty graphics protocol through.
	if os.Getenv("ZELLIJ") != "" {
		return imageModeANSI, "zellij blocks native graphics; run xeet directly in the terminal for sharp images"
	}
	// tmux 3.3+ forwards DCS-wrapped kitty graphics to the outer terminal when
	// allow-passthrough is on; every kitty escape below goes through tmuxWrap.
	// The Unicode-placeholder cells are ordinary text tmux redraws itself, so
	// images survive pane moves and window switches.
	if insideTmux() {
		if requested == "native" {
			return imageModeNative, ""
		}
		if ok, note := tmuxNativeSupport(); !ok {
			return imageModeANSI, note + " (--images native to force)"
		}
		return imageModeNative, ""
	}
	program := strings.ToLower(strings.TrimSpace(os.Getenv("TERM_PROGRAM")))
	term := strings.ToLower(os.Getenv("TERM"))
	lcTerminal := strings.ToLower(os.Getenv("LC_TERMINAL"))
	// WezTerm and iTerm2 both implement the iTerm2 inline-image protocol.
	// Check their terminal-specific variables too because TERM_PROGRAM is not
	// consistently preserved by shells and launchers.
	if program == "wezterm" || program == "iterm.app" || program == "iterm2" ||
		strings.Contains(term, "wezterm") || strings.Contains(lcTerminal, "iterm") ||
		os.Getenv("WEZTERM_PANE") != "" || os.Getenv("WEZTERM_EXECUTABLE") != "" ||
		os.Getenv("ITERM_SESSION_ID") != "" {
		return imageModeWezTerm, ""
	}
	if program == "ghostty" || os.Getenv("KITTY_WINDOW_ID") != "" || strings.Contains(term, "ghostty") || strings.Contains(term, "kitty") {
		// Apps that embed libghostty (cmux, for one) inherit a ghostty TERM
		// and even acknowledge graphics queries, but their own rendering
		// layer doesn't reliably draw Unicode placeholders. On macOS the
		// launching app's bundle identifier exposes them.
		if bundle := os.Getenv("__CFBundleIdentifier"); requested != "native" &&
			bundle != "" && bundle != "com.mitchellh.ghostty" && bundle != "net.kovidgoyal.kitty" {
			return imageModeANSI, bundle + " advertises ghostty/kitty graphics it may not render; using ansi (--images native to force)"
		}
		return imageModeNative, ""
	}
	return imageModeANSI, "this terminal has no native image protocol"
}

func (m *Model) requestPreviews() tea.Cmd {
	c := m.cur()
	posts := m.activePosts()
	if m.imageMode == imageModeOff || len(posts) == 0 || c.selected < 0 || c.selected >= len(posts) {
		return nil
	}
	width := max(12, m.contentWidth()-4)
	if m.mode == modeThread {
		// Replies sit behind a rail, so reserve the deepest indent for every
		// post in the conversation. One width for all of them keeps a cached
		// preview usable at whatever depth a later page settles its post on.
		width = max(12, m.contentWidth()-4-2*maxRailDepth)
	}
	rows := min(16, max(3, c.viewport.Height-5))
	if m.imageMode == imageModeNative {
		// Kitty Unicode placeholders encode cell coordinates through the
		// diacritics table, so native previews cannot exceed its size.
		width = min(width, len(kittyDiacritics))
	}

	var cmds []tea.Cmd
	selectedChanged := false
	from := max(0, c.selected-prefetchBehind)
	to := min(len(posts)-1, c.selected+prefetchAhead)
	if m.imageMode == imageModeNative || m.imageMode == imageModeWezTerm {
		// Native terminals can render every image currently visible without the
		// large text payload of ANSI previews. Include the viewport so images in
		// tall windows load before the cursor lands directly on their post.
		for i := range posts {
			if i >= len(c.starts) || i >= len(c.ends) {
				break
			}
			if c.ends[i] >= c.viewport.YOffset && c.starts[i] < c.viewport.YOffset+c.viewport.Height {
				from = min(from, max(0, i-1))
				to = max(to, min(len(posts)-1, i+1))
			}
		}
	}
	for i := from; i <= to; i++ {
		post := posts[i]
		if len(post.Media) == 0 {
			continue
		}
		if state, exists := m.previews[post.ID]; exists {
			// A preview cached at a wider frame -- from the feed, before a
			// thread indented its post behind a rail, or from before the window
			// shrank -- would spill past the right edge, so it is fetched again
			// at the width it now has to fit. Both renderers cap themselves at
			// the width they are given, so the refetch cannot repeat.
			tooWide := !state.loading && state.err == nil && previewColumns(state) > width
			// A failed fetch retries only once its post is selected again.
			if !tooWide && (i != c.selected || state.loading || state.err == nil) {
				continue
			}
		}
		m.previews[post.ID] = previewState{loading: true}
		if i == c.selected {
			selectedChanged = true
		}
		cmds = append(cmds, fetchPreview(m.requestContext(), post.ID, post.Media[0], width, rows, m.imageMode, c.id))
	}
	if selectedChanged {
		m.syncViewport()
		m.ensureSelectedVisible()
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func zoomKey(postID string) string { return "zoom:" + postID }

// requestZoom fetches the selected post's image sized for the whole
// terminal; the result lives in the preview cache under a zoom key.
func (m *Model) requestZoom() tea.Cmd {
	post, ok := m.currentPost()
	if !ok || len(post.Media) == 0 || m.imageMode == imageModeOff {
		return nil
	}
	key := zoomKey(post.ID)
	if _, exists := m.previews[key]; exists {
		return nil
	}
	m.previews[key] = previewState{loading: true}
	width := max(12, m.width-6)
	rows := max(4, m.height-5)
	if m.imageMode == imageModeNative {
		width = min(width, len(kittyDiacritics))
		rows = min(rows, len(kittyDiacritics))
	}
	return fetchPreview(m.requestContext(), key, post.Media[0], width, rows, m.imageMode, m.cur().id)
}

func (m Model) zoomLoading() bool {
	if !m.zoom {
		return false
	}
	post, ok := m.currentPost()
	if !ok {
		return false
	}
	return m.previews[zoomKey(post.ID)].loading
}

func (m *Model) evictDistantPreviews() {
	if len(m.previews) <= maxCachedPreviews {
		return
	}
	posts := m.activePosts()
	index := make(map[string]int, len(posts))
	for i := range posts {
		index[posts[i].ID] = i
	}
	activeZoom := ""
	if post, ok := m.currentPost(); ok && m.zoom {
		activeZoom = zoomKey(post.ID)
	}
	// Terminal-side data for an evicted native preview is reclaimed when its
	// 8-bit Kitty image ID is reused; only the temp file needs cleanup here.
	for id, preview := range m.previews {
		if preview.loading || id == activeZoom {
			continue
		}
		if _, inFeed := index[id]; inFeed {
			continue
		}
		m.evictPreview(id, preview)
	}
	for id, preview := range m.previews {
		if len(m.previews) <= maxCachedPreviews {
			return
		}
		if preview.loading || id == activeZoom {
			continue
		}
		if pos, ok := index[id]; ok && abs(pos-m.cur().selected) <= previewKeepRadius {
			continue
		}
		m.evictPreview(id, preview)
	}
}

func (m *Model) evictPreview(id string, preview previewState) {
	if preview.nativePath != "" {
		_ = os.Remove(preview.nativePath)
	}
	delete(m.previews, id)
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// fetchPreview hangs its timeout off the model's context so a download in
// flight when the program stops cannot write a native PNG after
// cleanupPreviews has already run, which would strand it in the temp dir for
// the rest of a compose handoff.
func fetchPreview(parent context.Context, postID string, media api.TimelineMedia, width, maxRows int, mode imageMode, colID int) tea.Cmd {
	return func() tea.Msg {
		parsed, err := url.Parse(previewMediaURL(media.URL, mode))
		if err != nil {
			return previewMsg{postID: postID, colID: colID, err: err}
		}
		if err := validateMediaURL(parsed); err != nil {
			return previewMsg{postID: postID, colID: colID, err: err}
		}
		ctx, cancel := context.WithTimeout(parent, 20*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return previewMsg{postID: postID, colID: colID, err: err}
		}
		req.Header.Set("User-Agent", "github.com/melqtx/xeet/terminal-image-preview")
		client := &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many image redirects")
				}
				return validateMediaURL(req.URL)
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			return previewMsg{postID: postID, colID: colID, err: fmt.Errorf("load image: %w", err)}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return previewMsg{postID: postID, colID: colID, err: fmt.Errorf("load image: HTTP %d", resp.StatusCode)}
		}
		if !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "image/") {
			return previewMsg{postID: postID, colID: colID, err: fmt.Errorf("media is not an image")}
		}
		imageData, err := io.ReadAll(io.LimitReader(resp.Body, maxPreviewBytes+1))
		if err != nil {
			return previewMsg{postID: postID, colID: colID, err: err}
		}
		if len(imageData) > maxPreviewBytes {
			return previewMsg{postID: postID, colID: colID, err: fmt.Errorf("image is too large to preview")}
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(imageData))
		if err != nil {
			return previewMsg{postID: postID, colID: colID, err: fmt.Errorf("decode image: %w", err)}
		}
		if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 50_000_000 {
			return previewMsg{postID: postID, colID: colID, err: fmt.Errorf("image dimensions are too large to preview")}
		}
		decoded, _, err := image.Decode(bytes.NewReader(imageData))
		if err != nil {
			return previewMsg{postID: postID, colID: colID, err: fmt.Errorf("decode image: %w", err)}
		}
		if mode == imageModeNative {
			columns, rows := nativeCellSize(decoded.Bounds(), width, maxRows)
			path, err := writePreviewPNG(downscaleForCells(decoded, columns, rows))
			if err != nil {
				return previewMsg{postID: postID, colID: colID, err: err}
			}
			return previewMsg{
				postID: postID, colID: colID, nativePath: path, imageID: previewImageID(postID),
				columns: columns, rows: rows,
			}
		}
		if mode == imageModeWezTerm {
			columns, rows := nativeCellSize(decoded.Bounds(), width, maxRows)
			encoded, err := encodePreviewPNG(downscaleForCells(decoded, columns, rows))
			if err != nil {
				return previewMsg{postID: postID, colID: colID, err: err}
			}
			return previewMsg{postID: postID, colID: colID, nativeData: encoded, columns: columns, rows: rows}
		}
		return previewMsg{postID: postID, colID: colID, content: renderANSIImage(decoded, width, maxRows)}
	}
}

// Pixel budget per terminal cell for native image payloads, generous enough
// for 2x/retina displays. Full-resolution photos previously went to the
// terminal untouched: a timeline of them exhausts the terminal's image
// storage quota, at which point kitty-protocol terminals silently evict the
// oldest transmissions and earlier previews disappear from the screen.
const (
	cellPixelWidth  = 20
	cellPixelHeight = 40
)

// downscaleForCells bounds an image to the pixel area its terminal cells can
// actually show. It never upscales.
func downscaleForCells(source image.Image, columns, rows int) image.Image {
	bounds := source.Bounds()
	maxWidth := columns * cellPixelWidth
	maxHeight := rows * cellPixelHeight
	if maxWidth <= 0 || maxHeight <= 0 || (bounds.Dx() <= maxWidth && bounds.Dy() <= maxHeight) {
		return source
	}
	width := maxWidth
	height := max(1, bounds.Dy()*width/bounds.Dx())
	if height > maxHeight {
		height = maxHeight
		width = max(1, bounds.Dx()*height/bounds.Dy())
	}
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(target, target.Bounds(), source, bounds, draw.Over, nil)
	return target
}

func encodePreviewPNG(source image.Image) (string, error) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		return "", fmt.Errorf("encode native image: %w", err)
	}
	return base64.StdEncoding.EncodeToString(encoded.Bytes()), nil
}

func writePreviewPNG(source image.Image) (string, error) {
	file, err := os.CreateTemp("", "xeet-preview-*.png")
	if err != nil {
		return "", fmt.Errorf("prepare native image: %w", err)
	}
	path := file.Name()
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := png.Encode(file, source); err != nil {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("encode native image: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func nativeCellSize(bounds image.Rectangle, maxColumns, maxRows int) (int, int) {
	columns := maxColumns
	pixelHeight := max(2, bounds.Dy()*columns/bounds.Dx())
	rows := max(1, (pixelHeight+1)/2)
	if rows > maxRows {
		rows = maxRows
		columns = max(1, bounds.Dx()*(rows*2)/bounds.Dy())
	}
	return columns, rows
}

func previewImageID(postID string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(postID))
	// Kitty placeholders support a 24-bit image ID through truecolor
	// foreground components. Restricting IDs to the 8-bit palette caused
	// frequent collisions as timeline previews accumulated, letting a later
	// transmission replace an earlier post's image inside the terminal.
	id := hash.Sum32() & 0xFFFFFF
	if id == 0 || id == probeImageID {
		id++
	}
	return id
}

func kittyImageColor(imageID uint32) (red, green, blue uint32) {
	// Kitty interprets the RGB foreground value as the lower 24 bits of the
	// image ID, in normal hexadecimal color order.
	return (imageID >> 16) & 0xFF, (imageID >> 8) & 0xFF, imageID & 0xFF
}

func kittyTransmit(path string, imageID uint32) string {
	payload := base64.StdEncoding.EncodeToString([]byte(path))
	return tmuxWrap(fmt.Sprintf("\x1b_Ga=t,f=100,t=f,i=%d,q=2;%s\x1b\\", imageID, payload))
}

func kittyVirtualPlacement(imageID uint32, columns, rows int) string {
	return tmuxWrap(fmt.Sprintf("\x1b_Ga=p,U=1,i=%d,c=%d,r=%d,q=2\x1b\\", imageID, columns, rows))
}

func wezTermDisplay(encoded string, columns, rows int) string {
	return fmt.Sprintf("\x1b]1337;File=width=%d;height=%d;inline=1;doNotMoveCursor=1:%s\x1b\\", columns, rows, encoded)
}

var kittyDiacritics = []rune{
	'\u0305', '\u030D', '\u030E', '\u0310', '\u0312', '\u033D', '\u033E', '\u033F',
	'\u0346', '\u034A', '\u034B', '\u034C', '\u0350', '\u0351', '\u0352', '\u0357',
	'\u035B', '\u0363', '\u0364', '\u0365', '\u0366', '\u0367', '\u0368', '\u0369',
	'\u036A', '\u036B', '\u036C', '\u036D', '\u036E', '\u036F', '\u0483', '\u0484',
	'\u0485', '\u0486', '\u0487', '\u0592', '\u0593', '\u0594', '\u0595', '\u0597',
	'\u0598', '\u0599', '\u059C', '\u059D', '\u059E', '\u059F', '\u05A0', '\u05A1',
	'\u05A8', '\u05A9', '\u05AB', '\u05AC', '\u05AF', '\u05C4', '\u0610', '\u0611',
	'\u0612', '\u0613', '\u0614', '\u0615', '\u0616', '\u0617', '\u0657', '\u0658',
}

func (m Model) nativePreviewBlock(preview previewState) string {
	if preview.nativePath == "" || preview.rows <= 0 || preview.columns <= 0 {
		return ""
	}
	columns := min(preview.columns, len(kittyDiacritics))
	rows := min(preview.rows, len(kittyDiacritics))
	var output strings.Builder
	output.WriteString(kittyTransmit(preview.nativePath, preview.imageID))
	red, green, blue := kittyImageColor(preview.imageID)
	for row := 0; row < rows; row++ {
		fmt.Fprintf(&output, "\x1b[38;2;%d;%d;%dm", red, green, blue)
		for column := 0; column < columns; column++ {
			output.WriteRune('\U0010EEEE')
			output.WriteRune(kittyDiacritics[row])
			output.WriteRune(kittyDiacritics[column])
		}
		output.WriteString("\x1b[39m")
		if row < rows-1 {
			output.WriteByte('\n')
		}
	}
	output.WriteString(kittyVirtualPlacement(preview.imageID, columns, rows))
	return output.String()
}

func (m Model) wezTermPreviewBlock(preview previewState) string {
	if preview.nativeData == "" || preview.rows <= 0 || preview.columns <= 0 {
		return ""
	}
	var output strings.Builder
	for row := 0; row < preview.rows; row++ {
		output.WriteString(strings.Repeat(" ", preview.columns))
		if row == preview.rows-1 {
			fmt.Fprintf(&output, "\x1b[%dD", preview.columns)
			if preview.rows > 1 {
				fmt.Fprintf(&output, "\x1b[%dA", preview.rows-1)
			}
			// Wrap the image emit in DECSC/DECRC. WezTerm honors
			// doNotMoveCursor=1, but iTerm2 moves the cursor after drawing an
			// inline image; without the restore every later relative movement
			// lands offset and the frame around the image corrupts.
			output.WriteString("\x1b7")
			output.WriteString(wezTermDisplay(preview.nativeData, preview.columns, preview.rows))
			output.WriteString("\x1b8")
			if preview.rows > 1 {
				fmt.Fprintf(&output, "\x1b[%dB", preview.rows-1)
			}
			fmt.Fprintf(&output, "\x1b[%dC", preview.columns)
		}
		if row < preview.rows-1 {
			output.WriteByte('\n')
		}
	}
	return output.String()
}

func (m *Model) cleanupPreviews() {
	for _, preview := range m.previews {
		if preview.nativePath != "" {
			_ = os.Remove(preview.nativePath)
		}
	}
}

func validateMediaURL(parsed *url.URL) error {
	if parsed.Scheme != "https" {
		return fmt.Errorf("only HTTPS image URLs are allowed")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "pbs.twimg.com" && host != "video.twimg.com" && !strings.HasSuffix(host, ".twimg.com") {
		return fmt.Errorf("untrusted image host %q", host)
	}
	return nil
}

func previewMediaURL(raw string, mode imageMode) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	if mode == imageModeNative || mode == imageModeWezTerm {
		query.Set("name", "large")
	} else {
		query.Set("name", "small")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func renderANSIImage(source image.Image, maxWidth, maxRows int) string {
	bounds := source.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 || maxWidth <= 0 || maxRows <= 0 {
		return ""
	}
	width := maxWidth
	pixelHeight := max(2, bounds.Dy()*width/bounds.Dx())
	if pixelHeight > maxRows*2 {
		pixelHeight = maxRows * 2
		width = max(1, bounds.Dx()*pixelHeight/bounds.Dy())
	}
	rows := max(1, (pixelHeight+1)/2)
	destination := image.NewNRGBA(image.Rect(0, 0, width, rows*2))
	draw.CatmullRom.Scale(destination, destination.Bounds(), source, bounds, draw.Over, nil)

	var output strings.Builder
	for row := 0; row < rows; row++ {
		for x := 0; x < width; x++ {
			top := flatten(destination.NRGBAAt(x, row*2))
			bottom := flatten(destination.NRGBAAt(x, row*2+1))
			fmt.Fprintf(&output, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", top.R, top.G, top.B, bottom.R, bottom.G, bottom.B)
		}
		output.WriteString("\x1b[0m")
		if row < rows-1 {
			output.WriteByte('\n')
		}
	}
	return output.String()
}

func flatten(value color.NRGBA) color.NRGBA {
	if value.A == 255 {
		return value
	}
	const background = 17
	alpha := int(value.A)
	blend := func(channel uint8) uint8 {
		return uint8((int(channel)*alpha + background*(255-alpha)) / 255)
	}
	return color.NRGBA{R: blend(value.R), G: blend(value.G), B: blend(value.B), A: 255}
}
