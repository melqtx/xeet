package timeline

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"

	"xeet/pkg/api"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderANSIImageFitsBounds(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x * 3), G: uint8(y * 6), B: 120, A: 255})
		}
	}
	preview := renderANSIImage(img, 32, 8)
	if width := lipgloss.Width(preview); width > 32 {
		t.Fatalf("preview width=%d", width)
	}
	if height := lipgloss.Height(preview); height > 8 || height == 0 {
		t.Fatalf("preview height=%d", height)
	}
	if !strings.Contains(preview, "\x1b[38;2;") {
		t.Fatal("preview has no truecolor output")
	}
}

func TestNativeModeDetection(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("TMUX", "")
	t.Setenv("__CFBundleIdentifier", "")
	t.Setenv("TERM_PROGRAM", "ghostty")
	if got, note := resolveImageMode("auto"); got != imageModeNative || note != "" {
		t.Fatalf("direct Ghostty resolved to %q (%q)", got, note)
	}
	t.Setenv("ZELLIJ", "0")
	if got, note := resolveImageMode("auto"); got != imageModeANSI || note == "" {
		t.Fatalf("Ghostty through Zellij resolved to %q without a downgrade note", got)
	}
	t.Setenv("ZELLIJ", "")
	t.Setenv("TERM_PROGRAM", "WezTerm")
	if got, _ := resolveImageMode("auto"); got != imageModeWezTerm {
		t.Fatalf("WezTerm resolved to %q", got)
	}
	if got, _ := resolveImageMode("native"); got != imageModeWezTerm {
		t.Fatalf("WezTerm native mode resolved to %q", got)
	}
}

func TestEmbeddedGhosttyHostDowngrades(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("TERM", "xterm-ghostty")
	t.Setenv("__CFBundleIdentifier", "com.example.cmux")
	if got, note := resolveImageMode("auto"); got != imageModeANSI || note == "" {
		t.Fatalf("libghostty embedder resolved to %q without a downgrade note", got)
	}
	if got, _ := resolveImageMode("native"); got != imageModeNative {
		t.Fatalf("--images native inside an embedder resolved to %q", got)
	}
	t.Setenv("__CFBundleIdentifier", "com.mitchellh.ghostty")
	if got, note := resolveImageMode("auto"); got != imageModeNative || note != "" {
		t.Fatalf("real Ghostty resolved to %q (%q)", got, note)
	}
	t.Setenv("__CFBundleIdentifier", "net.kovidgoyal.kitty")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TERM", "xterm-kitty")
	if got, note := resolveImageMode("auto"); got != imageModeNative || note != "" {
		t.Fatalf("real kitty resolved to %q (%q)", got, note)
	}
}

func TestKittyPlaceholderBlockClampsToDiacriticsTable(t *testing.T) {
	m := NewWithImageMode("native")
	// Regression: previews wider than the 64-rune diacritics table used to
	// panic with an index out of range while rendering.
	preview := previewState{nativePath: "/tmp/image.png", imageID: 9, columns: 72, rows: 70}
	block := m.nativePreviewBlock(preview)
	if width := lipgloss.Width(block); width != len(kittyDiacritics) {
		t.Fatalf("oversized block width=%d", width)
	}
	if height := lipgloss.Height(block); height != len(kittyDiacritics) {
		t.Fatalf("oversized block height=%d", height)
	}
}

func TestZoomOpensFetchesAndCloses(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = mediaPosts(2)
	m.syncViewport()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = next.(Model)
	if !m.zoom || cmd == nil {
		t.Fatal("i did not open the zoom view")
	}
	if !m.previews[zoomKey("p0")].loading {
		t.Fatal("zoom image was not requested")
	}
	if view := m.View(); !strings.Contains(view, "loading image") {
		t.Fatalf("zoom view missing loading state:\n%s", view)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)
	if m.selected != 0 {
		t.Fatal("feed keys leaked through the zoom view")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.zoom {
		t.Fatal("esc did not close the zoom view")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = next.(Model)
	if !m.zoom {
		t.Fatal("second zoom did not open")
	}
}

func TestZoomWithoutMediaIsIgnored(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "1", Text: "text only", Handle: "cat"}}
	m.syncViewport()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = next.(Model)
	if m.zoom {
		t.Fatal("zoom opened on a post without media")
	}
}

func TestKittyPlaceholderBlockOccupiesCells(t *testing.T) {
	m := NewWithImageMode("native")
	preview := previewState{nativePath: "/tmp/image.png", imageID: 42, columns: 30, rows: 8}
	block := m.nativePreviewBlock(preview)
	if height := lipgloss.Height(block); height != 8 {
		t.Fatalf("native block height=%d", height)
	}
	if width := lipgloss.Width(block); width != 30 {
		t.Fatalf("native placeholder block width=%d", width)
	}
}

func TestWezTermBlockReservesAndRestoresCells(t *testing.T) {
	m := NewWithImageMode("auto")
	preview := previewState{nativeData: "aGVsbG8=", columns: 20, rows: 4}
	block := m.wezTermPreviewBlock(preview)
	if width := lipgloss.Width(block); width != 20 {
		t.Fatalf("wezterm block width=%d", width)
	}
	if height := lipgloss.Height(block); height != 4 {
		t.Fatalf("wezterm block height=%d", height)
	}
	if !strings.Contains(block, "\x1b]1337;File=width=20") || !strings.Contains(block, "doNotMoveCursor=1") {
		t.Fatal("wezterm image command missing")
	}
	lastLine := strings.Split(block, "\n")[3]
	if truncated := ansi.Truncate(lastLine, 80, ""); !strings.Contains(truncated, "\x1b]1337;File=") {
		t.Fatal("renderer truncation removed wezterm image command")
	}
}

func TestWezTermTimelineFrameKeepsStableHeight(t *testing.T) {
	m := New()
	m.imageMode = imageModeWezTerm
	m.loading = false
	m.posts = []api.TimelinePost{{
		ID: "1", AuthorName: "Alice", Handle: "alice", Text: "image post",
		Media: []api.TimelineMedia{{URL: "https://pbs.twimg.com/media/a"}},
	}}
	m.previews["1"] = previewState{nativeData: "aGVsbG8=", columns: 20, rows: 4}
	m.width, m.height = 80, 24
	m.resize()
	view := m.View()
	if lines := strings.Count(view, "\n") + 1; lines != 24 {
		t.Fatalf("wezterm frame has %d lines", lines)
	}
	if strings.Count(view, "Alice") != 1 {
		t.Fatal("wezterm frame duplicated timeline rows")
	}
}

func TestBubbleTeaTruncationPreservesNativePlacement(t *testing.T) {
	m := NewWithImageMode("native")
	preview := previewState{nativePath: "/tmp/image.png", imageID: 7, columns: 20, rows: 4}
	firstLine := strings.Split(m.nativePreviewBlock(preview), "\n")[0]
	truncated := ansi.Truncate(firstLine, 80, "")
	if !strings.Contains(truncated, "\x1b_Ga=t") || !strings.ContainsRune(truncated, '\U0010EEEE') {
		t.Fatal("renderer truncation removed native graphics data")
	}
}

func TestSelectedPostRequestsInlinePreview(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = []api.TimelinePost{{
		ID: "123", Text: "photo",
		Media: []api.TimelineMedia{{URL: "https://pbs.twimg.com/media/abc", Type: "photo"}},
	}}
	m.syncViewport()
	cmd := m.requestPreviews()
	if cmd == nil || !m.previews["123"].loading {
		t.Fatal("inline preview was not requested")
	}
	if !strings.Contains(m.viewport.View(), "loading image") {
		t.Fatal("loading state is not rendered in the timeline")
	}
}

func mediaPosts(count int) []api.TimelinePost {
	result := make([]api.TimelinePost, count)
	for i := range result {
		result[i] = api.TimelinePost{
			ID: fmt.Sprintf("p%d", i), Text: "photo", Handle: "cat",
			Media: []api.TimelineMedia{{URL: "https://pbs.twimg.com/media/abc", Type: "photo"}},
		}
	}
	return result
}

func TestPreviewsPrefetchAroundSelection(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = mediaPosts(20)
	m.selected = 5
	m.syncViewport()
	if cmd := m.requestPreviews(); cmd == nil {
		t.Fatal("prefetch requested nothing")
	}
	for i := m.selected - prefetchBehind; i <= m.selected+prefetchAhead; i++ {
		if !m.previews[fmt.Sprintf("p%d", i)].loading {
			t.Fatalf("post %d was not prefetched", i)
		}
	}
	if len(m.previews) != prefetchBehind+prefetchAhead+1 {
		t.Fatalf("prefetched %d previews", len(m.previews))
	}
	// A second pass must not refetch what is already cached or in flight.
	if cmd := m.requestPreviews(); cmd != nil {
		t.Fatal("prefetch refetched cached previews")
	}
}

func TestFailedPreviewRetriesOnlyWhenSelected(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = mediaPosts(3)
	m.selected = 0
	m.syncViewport()
	m.previews["p0"] = previewState{err: fmt.Errorf("boom")}
	m.previews["p1"] = previewState{err: fmt.Errorf("boom")}
	if cmd := m.requestPreviews(); cmd == nil {
		t.Fatal("selected failed preview did not retry")
	}
	if !m.previews["p0"].loading {
		t.Fatal("selected failed preview was not re-requested")
	}
	if m.previews["p1"].loading {
		t.Fatal("unselected failed preview should not retry")
	}
}

func TestPreviewCacheEvictsDistantEntries(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = mediaPosts(80)
	m.selected = 70
	for i := 0; i < 80; i++ {
		m.previews[fmt.Sprintf("p%d", i)] = previewState{content: "img"}
	}
	m.previews["gone-from-feed"] = previewState{content: "img"}
	m.evictDistantPreviews()
	if len(m.previews) > maxCachedPreviews {
		t.Fatalf("cache holds %d entries", len(m.previews))
	}
	for i := m.selected - previewKeepRadius; i <= 79; i++ {
		if _, ok := m.previews[fmt.Sprintf("p%d", i)]; !ok {
			t.Fatalf("evicted preview %d near the selection", i)
		}
	}
	if _, ok := m.previews["gone-from-feed"]; ok {
		t.Fatal("orphaned preview survived eviction")
	}
}
