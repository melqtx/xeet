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

	"xeet/pkg/api"

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
	imageModeWezTerm imageMode = "wezterm"
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
	// tmux requires explicit passthrough configuration we cannot assume.
	if os.Getenv("ZELLIJ") != "" || os.Getenv("TMUX") != "" {
		return imageModeANSI, "tmux/zellij blocks native graphics — run xeet directly in the terminal for sharp images"
	}
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	term := strings.ToLower(os.Getenv("TERM"))
	// WezTerm does not support Kitty Unicode placeholders, but it does support
	// the iTerm2 inline-image protocol through its own native renderer.
	if program == "wezterm" || strings.Contains(term, "wezterm") {
		return imageModeWezTerm, ""
	}
	if program == "ghostty" || os.Getenv("KITTY_WINDOW_ID") != "" || strings.Contains(term, "ghostty") || strings.Contains(term, "kitty") {
		// Apps that embed libghostty (cmux, for one) inherit a ghostty TERM
		// and even acknowledge graphics queries, but their own rendering
		// layer doesn't reliably draw Unicode placeholders. On macOS the
		// launching app's bundle identifier exposes them.
		if bundle := os.Getenv("__CFBundleIdentifier"); requested != "native" &&
			bundle != "" && bundle != "com.mitchellh.ghostty" && bundle != "net.kovidgoyal.kitty" {
			return imageModeANSI, bundle + " advertises ghostty/kitty graphics it may not render — using ansi (--images native to force)"
		}
		return imageModeNative, ""
	}
	return imageModeANSI, "this terminal has no native image protocol"
}

func (m *Model) requestPreviews() tea.Cmd {
	if m.imageMode == imageModeOff || len(m.posts) == 0 || m.selected < 0 || m.selected >= len(m.posts) {
		return nil
	}
	width := max(12, m.contentWidth()-4)
	rows := min(16, max(3, m.viewport.Height-5))
	if m.imageMode == imageModeNative {
		// Kitty Unicode placeholders encode cell coordinates through the
		// diacritics table, so native previews cannot exceed its size.
		width = min(width, len(kittyDiacritics))
	}

	var cmds []tea.Cmd
	selectedChanged := false
	from := max(0, m.selected-prefetchBehind)
	to := min(len(m.posts)-1, m.selected+prefetchAhead)
	for i := from; i <= to; i++ {
		post := m.posts[i]
		if len(post.Media) == 0 {
			continue
		}
		if state, exists := m.previews[post.ID]; exists {
			// A failed fetch retries only once its post is selected again.
			if i != m.selected || state.loading || state.err == nil {
				continue
			}
		}
		m.previews[post.ID] = previewState{loading: true}
		if i == m.selected {
			selectedChanged = true
		}
		cmds = append(cmds, fetchPreview(post.ID, post.Media[0], width, rows, m.imageMode))
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
	return fetchPreview(key, post.Media[0], width, rows, m.imageMode)
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
	index := make(map[string]int, len(m.posts))
	for i := range m.posts {
		index[m.posts[i].ID] = i
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
		if pos, ok := index[id]; ok && abs(pos-m.selected) <= previewKeepRadius {
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

func fetchPreview(postID string, media api.TimelineMedia, width, maxRows int, mode imageMode) tea.Cmd {
	return func() tea.Msg {
		parsed, err := url.Parse(previewMediaURL(media.URL, mode))
		if err != nil {
			return previewMsg{postID: postID, err: err}
		}
		if err := validateMediaURL(parsed); err != nil {
			return previewMsg{postID: postID, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return previewMsg{postID: postID, err: err}
		}
		req.Header.Set("User-Agent", "xeet/terminal-image-preview")
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
			return previewMsg{postID: postID, err: fmt.Errorf("load image: %w", err)}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return previewMsg{postID: postID, err: fmt.Errorf("load image: HTTP %d", resp.StatusCode)}
		}
		if !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "image/") {
			return previewMsg{postID: postID, err: fmt.Errorf("media is not an image")}
		}
		imageData, err := io.ReadAll(io.LimitReader(resp.Body, maxPreviewBytes+1))
		if err != nil {
			return previewMsg{postID: postID, err: err}
		}
		if len(imageData) > maxPreviewBytes {
			return previewMsg{postID: postID, err: fmt.Errorf("image is too large to preview")}
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(imageData))
		if err != nil {
			return previewMsg{postID: postID, err: fmt.Errorf("decode image: %w", err)}
		}
		if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 50_000_000 {
			return previewMsg{postID: postID, err: fmt.Errorf("image dimensions are too large to preview")}
		}
		decoded, _, err := image.Decode(bytes.NewReader(imageData))
		if err != nil {
			return previewMsg{postID: postID, err: fmt.Errorf("decode image: %w", err)}
		}
		if mode == imageModeNative {
			path, err := writePreviewPNG(decoded)
			if err != nil {
				return previewMsg{postID: postID, err: err}
			}
			columns, rows := nativeCellSize(decoded.Bounds(), width, maxRows)
			return previewMsg{
				postID: postID, nativePath: path, imageID: previewImageID(postID),
				columns: columns, rows: rows,
			}
		}
		if mode == imageModeWezTerm {
			encoded, err := encodePreviewPNG(decoded)
			if err != nil {
				return previewMsg{postID: postID, err: err}
			}
			columns, rows := nativeCellSize(decoded.Bounds(), width, maxRows)
			return previewMsg{postID: postID, nativeData: encoded, columns: columns, rows: rows}
		}
		return previewMsg{postID: postID, content: renderANSIImage(decoded, width, maxRows)}
	}
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
	// Indexed foreground colors encode the image ID in Unicode placeholders.
	// Keep IDs in the unambiguous 1..255 range; only cached timeline previews
	// coexist, so collisions are exceptionally unlikely and harmless.
	return hash.Sum32()%255 + 1
}

func kittyTransmit(path string, imageID uint32) string {
	payload := base64.StdEncoding.EncodeToString([]byte(path))
	return fmt.Sprintf("\x1b_Ga=t,f=100,t=f,i=%d,q=2;%s\x1b\\", imageID, payload)
}

func kittyVirtualPlacement(imageID uint32, columns, rows int) string {
	return fmt.Sprintf("\x1b_Ga=p,U=1,i=%d,c=%d,r=%d,q=2\x1b\\", imageID, columns, rows)
}

func wezTermDisplay(encoded string, columns, rows int) string {
	return fmt.Sprintf("\x1b]1337;File=width=%d;height=%d;inline=1;doNotMoveCursor=1:%s\x1b\\", columns, rows, encoded)
}

func kittyDelete(imageID uint32) string {
	return fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", imageID)
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
	for row := 0; row < rows; row++ {
		fmt.Fprintf(&output, "\x1b[38;5;%dm", preview.imageID)
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
			output.WriteString(wezTermDisplay(preview.nativeData, preview.columns, preview.rows))
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

func (m Model) clearNativeImages() string {
	if m.imageMode != imageModeNative {
		return ""
	}
	var commands strings.Builder
	for _, preview := range m.previews {
		if preview.nativePath != "" {
			commands.WriteString(kittyDelete(preview.imageID))
		}
	}
	return commands.String()
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
