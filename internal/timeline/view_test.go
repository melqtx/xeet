package timeline

import (
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/api"
)

func TestFormatCount(t *testing.T) {
	cases := map[int]string{
		0:          "0",
		42:         "42",
		999:        "999",
		1000:       "1k",
		1499:       "1.4k",
		9950:       "9.9k",
		12345:      "12k",
		999999:     "999k",
		1_000_000:  "1m",
		1_230_000:  "1.2m",
		25_400_000: "25m",
	}
	for value, want := range cases {
		if got := formatCount(value); got != want {
			t.Errorf("formatCount(%d)=%q want %q", value, got, want)
		}
	}
}

func TestStripTrailingMediaLink(t *testing.T) {
	cases := map[string]string{
		"look at this https://t.co/AbC123":           "look at this",
		"https://t.co/AbC123":                        "",
		"link inside https://t.co/AbC123 and after":  "link inside https://t.co/AbC123 and after",
		"no links at all":                            "no links at all",
		"two https://t.co/first https://t.co/second": "two https://t.co/first",
	}
	for input, want := range cases {
		if got := stripTrailingMediaLink(input); got != want {
			t.Errorf("stripTrailingMediaLink(%q)=%q want %q", input, got, want)
		}
	}
}

func TestHighlightEntitiesShortensLinks(t *testing.T) {
	got := highlightEntities("see https://example.com/a and @cat #tui", dim)
	if strings.Contains(got, "https://") {
		t.Fatalf("link scheme was not stripped: %q", got)
	}
	for _, want := range []string{"example.com/a", "@cat", "#tui"} {
		if !strings.Contains(got, want) {
			t.Fatalf("highlighted text lost %q: %q", want, got)
		}
	}
}

func TestMediaChip(t *testing.T) {
	photo := api.TimelineMedia{URL: "https://pbs.twimg.com/a", Type: "photo"}
	cases := []struct {
		post api.TimelinePost
		want string
	}{
		{api.TimelinePost{Media: []api.TimelineMedia{photo}}, "▣ image"},
		{api.TimelinePost{Media: []api.TimelineMedia{photo, photo, photo}}, "▣ 3 images"},
		{api.TimelinePost{Media: []api.TimelineMedia{{Type: "video"}}}, "▶ video"},
		{api.TimelinePost{Media: []api.TimelineMedia{{Type: "animated_gif"}}}, "▶ gif"},
	}
	for _, tc := range cases {
		if got := mediaChip(tc.post); got != tc.want {
			t.Errorf("mediaChip=%q want %q", got, tc.want)
		}
	}
}

func TestUnselectedPostNearSelectionRendersCachedImage(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = mediaPosts(10)
	m.selected = 0
	m.previews["p1"] = previewState{content: "CACHED-IMAGE-BLOCK"}
	m.previews["p9"] = previewState{content: "FAR-IMAGE-BLOCK"}
	m.syncViewport()
	content, _, _ := m.renderFeedContent()
	if !strings.Contains(content, "CACHED-IMAGE-BLOCK") {
		t.Fatal("cached image near the selection did not render inline")
	}
	if strings.Contains(content, "FAR-IMAGE-BLOCK") {
		t.Fatal("image outside the inline radius rendered inline")
	}
	if !strings.Contains(content, "▣ image") {
		t.Fatal("distant media post lost its chip")
	}
}

func TestSelectedImageRowsCarryGutter(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = mediaPosts(1)
	m.selected = 0
	m.previews["p0"] = previewState{content: "IMGROW1\nIMGROW2\nIMGROW3"}
	m.syncViewport()
	content, _, _ := m.renderFeedContent()
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "IMGROW") && !strings.Contains(line, "▎") {
			t.Fatalf("image row lost the selection gutter: %q", line)
		}
	}
}

func TestMultiImagePostShowsCountUnderPreview(t *testing.T) {
	m := New()
	m.loading = false
	posts := mediaPosts(1)
	posts[0].Media = append(posts[0].Media, posts[0].Media[0], posts[0].Media[0])
	m.posts = posts
	m.selected = 0
	m.previews["p0"] = previewState{content: "IMGROW"}
	m.syncViewport()
	content, _, _ := m.renderFeedContent()
	if !strings.Contains(content, "▣ 1/3 · i zoom") {
		t.Fatalf("selected multi-image post is missing its count caption:\n%s", content)
	}

	m.posts = append(mediaPosts(1), posts[0])
	m.posts[1].ID = "p1"
	m.previews["p1"] = previewState{content: "IMGROW"}
	m.syncViewport()
	content, _, _ = m.renderFeedContent()
	if !strings.Contains(content, "▣ 1/3") || strings.Contains(content, "1/3 · i zoom") {
		t.Fatalf("unselected multi-image caption should drop the zoom hint:\n%s", content)
	}
}

func TestImageBlockIsPaddedWithGutterLines(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = mediaPosts(1)
	m.selected = 0
	m.previews["p0"] = previewState{content: "IMGROW"}
	m.syncViewport()
	content, _, _ := m.renderFeedContent()
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "IMGROW") {
			continue
		}
		if i == 0 || i == len(lines)-1 {
			t.Fatalf("image block has no room for spacer lines:\n%s", content)
		}
		above, below := lines[i-1], lines[i+1]
		for _, spacer := range []string{above, below} {
			if !strings.Contains(spacer, "▎") || strings.TrimSpace(strings.ReplaceAll(spacer, "▎", "")) != "" {
				t.Fatalf("image is not framed by blank gutter lines:\n%s", content)
			}
		}
	}
}

func TestHelpShowsImageModeNote(t *testing.T) {
	m := New()
	m.imageMode = imageModeANSI
	m.imageNote = "tmux/zellij blocks native graphics; run xeet directly in the terminal for sharp images"
	m.width, m.height = 80, 30
	m.help = true
	view := m.View()
	if !strings.Contains(view, "images: ansi") || !strings.Contains(view, "tmux/zellij blocks") {
		t.Fatalf("help overlay is missing the image mode note:\n%s", view)
	}
}

func TestUnselectedMediaPostShowsChip(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = []api.TimelinePost{
		{ID: "1", AuthorName: "Alice", Handle: "alice", Text: "words"},
		{ID: "2", AuthorName: "Bob", Handle: "bob", Text: "pic https://t.co/x",
			Media: []api.TimelineMedia{{URL: "https://pbs.twimg.com/a", Type: "photo"}}},
	}
	m.syncViewport()
	view := m.viewport.View()
	if !strings.Contains(view, "▣ image") {
		t.Fatalf("unselected media post has no chip:\n%s", view)
	}
	if strings.Contains(view, "t.co/x") {
		t.Fatalf("trailing media link was rendered:\n%s", view)
	}
}
