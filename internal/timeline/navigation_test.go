package timeline

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/melqtx/xeet/pkg/api"
)

func TestFeedTabsRestoreReadingPosition(t *testing.T) {
	m := NewWithImageMode("off")
	m.width, m.height = 80, 24
	m.loading = false
	m.posts = posts(20)
	m.cursor = "home-next"
	m.resize()
	m.moveSelection(8)
	m.expanded = true
	offset := m.viewport.YOffset
	m.setFeed(FeedFollowing)
	m = update(t, m, pageMsg{seq: m.feedSeq, page: &api.TimelinePage{Posts: []api.TimelinePost{{ID: "following", Text: "following post"}}, Cursor: "following-next"}})
	m.setFeed(FeedForYou)
	if m.loading || m.selected != 8 || !m.expanded || m.cursor != "home-next" || m.viewport.YOffset != offset || len(m.posts) != 20 {
		t.Fatalf("restored selected=%d offset=%d cursor=%s loading=%v", m.selected, m.viewport.YOffset, m.cursor, m.loading)
	}
	m.setFeed(FeedFollowing)
	if m.loading || len(m.posts) != 1 || m.posts[0].ID != "following" || m.cursor != "following-next" {
		t.Fatal("following tab not restored")
	}
	seq := m.feedSeq
	if cmd := m.setFeed(FeedFollowing); cmd != nil || seq != m.feedSeq {
		t.Fatal("active tab refetched")
	}
}

func TestFeedTabsIgnoreLateResponsesAndRestoreEmptyFeeds(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	oldSeq := m.feedSeq
	m.setFeed(FeedFollowing)
	m = update(t, m, pageMsg{seq: oldSeq, page: &api.TimelinePage{Posts: posts(2)}})
	if len(m.posts) != 0 || !m.loading {
		t.Fatal("old feed response leaked into active tab")
	}
	m.setFeed(FeedForYou)
	if m.loading || len(m.posts) != 0 {
		t.Fatal("loaded empty feed should restore")
	}
}

func TestRefreshSupersedesPaginationAndDoesNotOverlap(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.posts = posts(2)
	m.cursor = "next"
	m.loadingMore = true
	oldSeq := m.feedSeq
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	seq := m.feedSeq
	if !m.refreshing || m.loadingMore || seq == oldSeq || m.maybeLoadMore() != nil {
		t.Fatal("refresh did not supersede pagination")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if m.feedSeq != seq {
		t.Fatal("repeat key launched another refresh")
	}
	m = update(t, m, pageMsg{seq: oldSeq, more: true, page: &api.TimelinePage{Posts: posts(10)}})
	if len(m.posts) != 2 || !m.refreshing {
		t.Fatal("late pagination changed refreshed feed")
	}
	m = update(t, m, pageMsg{seq: seq, page: &api.TimelinePage{Posts: posts(2)}})
	if m.refreshing {
		t.Fatal("refresh did not settle")
	}
}

func TestLikeRollbackUpdatesInactiveTabs(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.posts = posts(1)
	m.applyLike("a", true)
	m.setFeed(FeedFollowing)
	m.settleLike(likeMsg{id: "a", liked: true, err: errors.New("rejected")})
	m.setFeed(FeedForYou)
	if m.posts[0].Liked || m.posts[0].LikeCount != 0 {
		t.Fatal("inactive tab retained failed optimistic like")
	}
}

func TestNumericTabsDoNotConsumeSearchOrReplyText(t *testing.T) {
	m := NewWithImageMode("off")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if m.feed != FeedBookmarks {
		t.Fatal("3 should select bookmarks")
	}
	m.beginSearch()
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if m.mode != modeSearch || m.searchInput.Value() != "2" || m.feed != FeedBookmarks {
		t.Fatal("numeric shortcut intercepted search text")
	}
	next, _ := m.beginReply(api.TimelinePost{ID: "parent"})
	m = next.(Model)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if m.mode != modeReply || m.replyEditor.Value() != "1" {
		t.Fatal("numeric shortcut intercepted reply text")
	}
}

func TestTabsStayVisibleWhileRefreshingAndFitNarrowFrames(t *testing.T) {
	for _, width := range []int{34, 42, 60, 80, 120} {
		m := NewWithImageMode("off")
		m.width, m.height = width, 24
		m.feed = FeedFollowing
		m.loading = false
		m.refreshing = true
		m.resize()
		header := m.header(m.contentWidth())
		if !strings.Contains(ansi.Strip(header), "[following]") || !(strings.Contains(ansi.Strip(header), "bookmarks") || strings.Contains(ansi.Strip(header), "saved")) {
			t.Fatalf("tabs disappeared at width %d: %s", width, header)
		}
		if lipgloss.Height(header) != 2 || maxLineWidth(header) > m.contentWidth() {
			t.Fatalf("header overflow at width %d", width)
		}
		if maxLineWidth(m.View()) > width || lipgloss.Height(m.View()) > 24 {
			t.Fatalf("frame overflow at width %d", width)
		}
	}
}

func TestPostFormattingPreservesParagraphsAndRemovesTerminalControls(t *testing.T) {
	raw := "first paragraph\r\n\r\n  @someone wrote:\n\t#terminal\x1b[31m\x07"
	want := "first paragraph\n\n  @someone wrote:\n    #terminal"
	if got := cleanText(raw); got != want {
		t.Fatalf("clean text=%q", got)
	}
	if got := ansi.Strip(highlightEntities(want, bright)); got != want {
		t.Fatalf("highlighting flattened formatting: %q", got)
	}
	if got := stripTrailingMediaLink("caption\n\nhttps://t.co/image"); got != "caption" {
		t.Fatalf("media link=%q", got)
	}
	m := NewWithImageMode("off")
	m.expanded = true
	rendered := ansi.Strip(m.renderPost(api.TimelinePost{ID: "1", Handle: "someone", Text: "first paragraph\n\nsecond paragraph"}, true, false, feedDepth))
	lines := strings.Split(rendered, "\n")
	if len(lines) != 5 || strings.TrimSpace(lines[2]) != "▎" || !strings.Contains(lines[1], "first paragraph") || !strings.Contains(lines[3], "second paragraph") {
		t.Fatalf("paragraph breaks lost in rendered post:\n%s", rendered)
	}
}

func TestSearchFromThreadRetainsUnderlyingFeed(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.posts = posts(12)
	m.resize()
	m.moveSelection(7)
	selectedID := m.posts[7].ID
	next, _ := m.beginThread(m.posts[7], modeFeed)
	m = next.(Model)
	m.beginSearch()
	m.searchInput.SetValue("terminal design")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.feed != FeedSearch || m.mode != modeFeed {
		t.Fatal("search did not open")
	}
	m.setFeed(FeedForYou)
	if m.loading || m.selected != 7 || m.posts[m.selected].ID != selectedID {
		t.Fatal("thread search overwrote the underlying feed position")
	}
}

func TestExpandedPostScrollSurvivesBackgroundUpdatesAndTabSwitches(t *testing.T) {
	m := NewWithImageMode("off")
	m.width, m.height = 80, 24
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "long", Text: strings.Repeat("a paragraph\n", 70)}}
	m.resize()
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	offset := m.viewport.YOffset
	if offset == 0 || m.selected != 0 {
		t.Fatal("page down did not scroll within the post")
	}
	m.ensureSelectedVisible()
	if m.viewport.YOffset != offset {
		t.Fatal("background layout snapped the reader to the top")
	}
	m.setFeed(FeedFollowing)
	m.setFeed(FeedForYou)
	if !m.expanded || m.viewport.YOffset != offset {
		t.Fatal("tab switch lost expanded reading position")
	}
	for i := 0; i < 10; i++ {
		m.scrollExpanded(1)
	}
	if m.viewport.YOffset != m.ends[0]-m.viewport.Height+1 {
		t.Fatal("cannot reach end of expanded post")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.viewport.YOffset >= m.ends[0]-m.viewport.Height+1 {
		t.Fatal("page up did not scroll back")
	}
}

func TestHelpFitsAndScrollsWithoutMovingFeed(t *testing.T) {
	for _, size := range [][2]int{{34, 12}, {42, 15}, {80, 24}, {80, 30}, {100, 45}} {
		m := NewWithImageMode("off")
		m.width, m.height = size[0], size[1]
		m.loading = false
		m.posts = posts(3)
		m.selected = 1
		m.help = true
		view := m.View()
		if maxLineWidth(view) > m.width || lipgloss.Height(view) > m.height {
			t.Fatalf("help overflow at %v: %dx%d", size, maxLineWidth(view), lipgloss.Height(view))
		}
		if m.helpMaxScroll() > 0 {
			m = update(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
			if m.helpScroll == 0 || m.selected != 1 {
				t.Fatal("help scrolling moved feed or did not scroll")
			}
		}
	}
}
