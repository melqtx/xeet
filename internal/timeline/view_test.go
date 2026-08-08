package timeline

import (
	"fmt"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/api"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func TestQuoteCardRendersTextAndInlinePreview(t *testing.T) {
	m := NewWithImageMode("off")
	m.width = 80
	quote := api.TimelinePost{
		ID: "quoted", AuthorName: "Quoted Author", Handle: "quoted", Text: "the quoted post",
		Media: []api.TimelineMedia{{URL: "https://pbs.twimg.com/media/quoted", Type: "photo"}},
	}
	m.posts = []api.TimelinePost{{ID: "outer", AuthorName: "Outer", Handle: "outer", Text: "my comment", Quote: &quote}}
	m.previews[quotePreviewKey("outer", "quoted")] = previewState{content: "QUOTE-IMAGE"}
	content, _, _ := m.renderFeedContent()
	plain := ansi.Strip(content)
	for _, want := range []string{"╭─ Quoted Author  @quoted", "the quoted post", "QUOTE-IMAGE", "╰─"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("quote card is missing %q:\n%s", want, plain)
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

// threadModel builds a conversation whose replies nest one level per post.
func threadModel(depths ...int) Model {
	m := NewWithImageMode("off")
	m.loading = false
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadPosts = []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", AuthorName: "Alice", Handle: "alice", Text: "the first word"}},
	}
	for i, depth := range depths {
		parent := m.threadPosts[len(m.threadPosts)-1].ID
		m.threadPosts = append(m.threadPosts, api.ConversationPost{
			TimelinePost: api.TimelinePost{
				ID: fmt.Sprintf("r%d", i), AuthorName: "Bob", Handle: fmt.Sprintf("bob%d", i),
				Text: "a reply", InReplyToID: parent,
			},
			Depth: depth,
		})
	}
	return m
}

func threadContent(t *testing.T, m Model) string {
	t.Helper()
	content, _, _ := m.renderThreadContent()
	return content
}

func threadLines(t *testing.T, m Model) []string {
	t.Helper()
	return strings.Split(threadContent(t, m), "\n")
}

// headerIndent is how far a post's header sits from the left edge, counting the
// rail and selection bar it hangs behind.
func headerIndent(t *testing.T, lines []string, handle string) int {
	t.Helper()
	for _, line := range lines {
		stripped := ansi.Strip(line)
		if !strings.Contains(stripped, handle) {
			continue
		}
		return lipgloss.Width(stripped) - lipgloss.Width(strings.TrimLeft(stripped, "│▎ "))
	}
	t.Fatalf("no header for %s in:\n%s", handle, strings.Join(lines, "\n"))
	return 0
}

func TestThreadRepliesIndentBehindTheRail(t *testing.T) {
	m := threadModel(1, 2)
	lines := threadLines(t, m)
	for handle, want := range map[string]int{"@alice": 2, "@bob0": 4, "@bob1": 6} {
		if got := headerIndent(t, lines, handle); got != want {
			t.Errorf("%s indents %d columns, want %d:\n%s", handle, got, want, strings.Join(lines, "\n"))
		}
	}

	// The feed is untouched by any of this.
	feed := NewWithImageMode("off")
	feed.loading = false
	feed.posts = []api.TimelinePost{{ID: "1", AuthorName: "Alice", Handle: "alice", Text: "the first word"}}
	content, _, _ := feed.renderFeedContent()
	if got := headerIndent(t, strings.Split(content, "\n"), "@alice"); got != 2 {
		t.Errorf("feed post indents %d columns, want 2:\n%s", got, content)
	}
	if strings.Contains(content, "│") {
		t.Errorf("a feed post grew a thread rail:\n%s", content)
	}
}

func TestThreadRailRunsThroughTheGapsBetweenPosts(t *testing.T) {
	m := threadModel(1, 1)
	lines := threadLines(t, m)
	for i, line := range lines {
		if strings.TrimSpace(ansi.Strip(line)) != "" {
			continue
		}
		t.Fatalf("line %d breaks the thread rail:\n%s", i, strings.Join(lines, "\n"))
	}
}

func TestSelectedReplyUsesTheReplyAccent(t *testing.T) {
	// lipgloss drops color outside a terminal, so the accent is checked where
	// it is chosen rather than in the rendered escape sequences.
	if selectionAccent(feedDepth) != blue || selectionAccent(0) != blue {
		t.Error("the feed and a thread's focal post should keep the feed accent")
	}
	if selectionAccent(1) != lavender || selectionAccent(9) != lavender {
		t.Error("replies should carry the reply accent")
	}
	if blue == lavender {
		t.Fatal("this theme cannot tell a reply from the post it answers")
	}

	// The bar itself lands on the selected post at its own indent.
	m := threadModel(1)
	m.selected = 1
	lines := threadLines(t, m)
	if got := headerIndent(t, lines, "@bob0"); got != 4 {
		t.Errorf("selected reply indents %d columns, want 4", got)
	}
	// The bar runs down the selected reply's block and nothing above it.
	header := 0
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), "@bob0") {
			header = i
		}
	}
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), "▎") != (i >= header) {
			t.Fatalf("line %d does not agree with the selection (header at %d):\n%s",
				i, header, strings.Join(lines, "\n"))
		}
	}
}

func TestDeepRepliesStayInsideTheFrame(t *testing.T) {
	m := threadModel(1, 2, 3, 4, 5)
	for i := range m.threadPosts {
		m.threadPosts[i].Text = strings.Repeat("wordy ", 40)
	}
	m.selected = len(m.threadPosts) - 1
	m.expanded = true
	for _, line := range threadLines(t, m) {
		if width := lipgloss.Width(line); width > m.contentWidth() {
			t.Fatalf("line is %d columns wide, frame is %d: %q", width, m.contentWidth(), ansi.Strip(line))
		}
	}
}

func TestReplyNamesTheParentTheRailCannotPlace(t *testing.T) {
	// Siblings answering the root need no label: the rail already says so.
	m := threadModel(1, 1)
	if content := threadContent(t, m); strings.Contains(content, "↳ @") {
		t.Fatalf("a direct reply to the root was labelled:\n%s", content)
	}

	// Nesting past the cap draws at the same indent as its parent, so the
	// parent has to be named instead.
	m = threadModel(1, 2, 3, 4)
	content := threadContent(t, m)
	if !strings.Contains(content, "↳ @bob2") {
		t.Fatalf("a reply nested past the rail cap did not name its parent:\n%s", content)
	}

	// A reply that arrives out of order does not sit under its parent.
	m = threadModel(1, 2, 2)
	m.threadPosts[3].InReplyToID = "r0"
	if content := threadContent(t, m); !strings.Contains(content, "↳ @bob0") {
		t.Fatalf("an out-of-order reply did not name its parent:\n%s", content)
	}
}

func TestIndentedReplyKeepsAWideCachedImageInsideTheFrame(t *testing.T) {
	m := threadModel(1, 2)
	m.imageMode = imageModeANSI
	last := len(m.threadPosts) - 1
	m.threadPosts[last].Media = []api.TimelineMedia{{URL: "https://pbs.twimg.com/media/abc", Type: "photo"}}
	// A preview cached while its post sat in the feed fills the whole frame.
	m.previews[m.threadPosts[last].ID] = previewState{content: strings.Repeat("X", m.contentWidth()-4)}
	m.selected = last
	for _, line := range threadLines(t, m) {
		if width := lipgloss.Width(line); width > m.contentWidth() {
			t.Fatalf("image row is %d columns wide, frame is %d: %q", width, m.contentWidth(), ansi.Strip(line))
		}
	}
}

func TestHeaderShowsFeedTabsAndNotificationIndicator(t *testing.T) {
	m := NewWithImageMode("off")
	m.width, m.height = 80, 24
	m.feed = FeedFollowing
	m.unreadNotifications = 3
	header := ansi.Strip(m.header(m.contentWidth()))
	for _, want := range []string{"for you", "[following]", "bookmarks", "n ●3"} {
		if !strings.Contains(header, want) {
			t.Fatalf("header missing %q:\n%s", want, header)
		}
	}
	if lipgloss.Height(header) != 2 {
		t.Fatalf("header grew to %d lines:\n%s", lipgloss.Height(header), header)
	}
}

func TestThreadHeaderNamesTheFocalAuthor(t *testing.T) {
	m := threadModel(1)
	m.width, m.height = 80, 24
	if header := m.header(m.contentWidth()); !strings.Contains(header, "replies to @alice") {
		t.Fatalf("thread header does not name the focal author:\n%s", header)
	}
	// A narrow frame keeps the status on one line rather than wrapping it.
	m.width, m.height = 34, 24
	header := m.header(m.contentWidth())
	if lipgloss.Height(header) != 2 {
		t.Fatalf("narrow thread header wrapped to %d lines:\n%s", lipgloss.Height(header), header)
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
