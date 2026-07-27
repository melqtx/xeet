package timeline

import (
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/api"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSearchPromptOpensFromFeedWithPreviousQuery(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.feed = FeedBookmarks
	m.searchQuery = "from:alice cats"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = next.(Model)

	if cmd == nil || m.mode != modeSearch || m.searchReturn != modeFeed {
		t.Fatalf("search prompt did not open from feed: mode=%v return=%v cmd=%v", m.mode, m.searchReturn, cmd)
	}
	if m.feed != FeedBookmarks {
		t.Fatalf("opening search changed the feed to %v; it should stay until a query is submitted", m.feed)
	}
	if got := m.searchInput.Value(); got != "from:alice cats" || !m.searchInput.Focused() {
		t.Fatalf("search input was not pre-filled and focused: value=%q focused=%v", got, m.searchInput.Focused())
	}
}

func TestSearchPromptOpensFromThreadAndEscRestoresThread(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.feed = FeedFollowing
	m.searchQuery = "original"
	m.mode = modeThread
	m.threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m.syncViewport()

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m.searchInput.SetValue("edited but cancelled")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.mode != modeThread || m.feed != FeedFollowing {
		t.Fatalf("esc did not restore the thread without changing feed: mode=%v feed=%v", m.mode, m.feed)
	}
	if m.searchQuery != "original" || m.searchInput.Focused() {
		t.Fatalf("esc changed the query or kept focus: query=%q focused=%v", m.searchQuery, m.searchInput.Focused())
	}
}

func TestEmptySearchSubmissionCancelsExactlyLikeEsc(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.feed = FeedBookmarks
	m.searchQuery = "keep me"
	m.mode = modeThread
	m.threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m.syncViewport()

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m.searchInput.SetValue(" \t ")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.mode != modeThread || m.feed != FeedBookmarks || m.searchQuery != "keep me" {
		t.Fatalf("empty enter did not cancel: mode=%v feed=%v query=%q", m.mode, m.feed, m.searchQuery)
	}
	if m.searchInput.Focused() {
		t.Fatal("empty enter left the search input focused")
	}
}

func TestNonEmptySearchSubmissionLeavesThreadForSearchFeed(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.feed = FeedBookmarks
	m.feedSeq = 4
	m.posts = []api.TimelinePost{{ID: "old", Text: "old"}}
	m.mode = modeThread
	m.threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m.syncViewport()

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m.searchInput.SetValue("  golang tui  ")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if cmd == nil || m.mode != modeFeed || m.feed != FeedSearch {
		t.Fatalf("search did not become a feed: mode=%v feed=%v cmd=%v", m.mode, m.feed, cmd)
	}
	if m.searchQuery != "golang tui" || m.feedSeq != 5 {
		t.Fatalf("search query or stale-page sequence is wrong: query=%q seq=%d", m.searchQuery, m.feedSeq)
	}
	if len(m.posts) != 0 || !m.loading || m.searchInput.Focused() {
		t.Fatalf("search feed was not reset for its first page: posts=%d loading=%v focused=%v", len(m.posts), m.loading, m.searchInput.Focused())
	}
}

func TestSearchPromptKeepsThreadSelectionDuringBackgroundFeedPage(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "a", Text: "a"}, {ID: "root", Text: "root"}}
	m.feedSelected = 1
	m.mode = modeThread
	m.threadPosts = []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}},
		{TimelinePost: api.TimelinePost{ID: "reply", Text: "reply"}, Depth: 1},
	}
	m.selected = 1
	m.syncViewport()

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: []api.TimelinePost{{ID: "new", Text: "new"}}}})

	if m.mode != modeSearch || m.selected != 1 || m.feedSelected != 2 {
		t.Fatalf("background feed page corrupted selections: mode=%v thread=%d feed=%d", m.mode, m.selected, m.feedSelected)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeThread || m.threadPosts[m.selected].ID != "reply" {
		t.Fatalf("cancel did not reveal the same thread selection: mode=%v selected=%d", m.mode, m.selected)
	}
}

func TestRefreshSearchFeedKeepsQuery(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.feed = FeedSearch
	m.searchQuery = "golang tui"
	m.posts = []api.TimelinePost{{ID: "result", Text: "result"}}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = next.(Model)

	if cmd == nil || !m.refreshing || m.feed != FeedSearch || m.searchQuery != "golang tui" {
		t.Fatalf("R did not refresh the same search: refreshing=%v feed=%v query=%q cmd=%v", m.refreshing, m.feed, m.searchQuery, cmd)
	}
}

func TestSearchViewAndHeaderExposeQueryWithinWidth(t *testing.T) {
	m := NewWithImageMode("off")
	m.width = 42
	m.height = 15
	m.loading = false
	m.feed = FeedSearch
	m.searchQuery = strings.Repeat("long query ", 10)
	m.beginSearch()

	view := m.View()
	for _, want := range []string{"search", "enter: search", "esc: cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("search prompt missing %q:\n%s", want, view)
		}
	}

	m.mode = modeFeed
	header := m.header(m.contentWidth())
	if width := maxLineWidth(header); width > m.contentWidth() {
		t.Fatalf("search header width=%d exceeds content width=%d:\n%s", width, m.contentWidth(), header)
	}
}

func maxLineWidth(value string) int {
	width := 0
	for _, line := range strings.Split(value, "\n") {
		width = max(width, ansi.StringWidth(line))
	}
	return width
}
