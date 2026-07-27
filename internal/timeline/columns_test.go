package timeline

import (
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/api"

	tea "github.com/charmbracelet/bubbletea"
)

func TestColumnContentWidths(t *testing.T) {
	tests := []struct {
		name       string
		totalWidth int
		columns    int
		want       int
	}{
		{name: "80 by 1", totalWidth: 80, columns: 1, want: 76},
		{name: "100 by 2", totalWidth: 100, columns: 2, want: 47},
		{name: "160 by 3", totalWidth: 160, columns: 3, want: 50},
		{name: "42 collapsed to 1", totalWidth: 42, columns: 1, want: 38},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := columnContentWidth(tt.totalWidth, tt.columns); got != tt.want {
				t.Fatalf("columnContentWidth(%d, %d)=%d, want %d", tt.totalWidth, tt.columns, got, tt.want)
			}
		})
	}

	m := New()
	m.width = 100
	m.configureColumns(2, FeedForYou, "", "")
	if got := m.contentWidth(); got != 47 {
		t.Fatalf("contentWidth()=%d, want delegated two-column width 47", got)
	}
}

func TestConfiguredColumnsStartOnTheSameSelectedFeed(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(4, FeedList, "", "123")

	for index := range m.columns {
		c := m.columns[index]
		if c.feed != FeedList || c.listID != "123" || c.listName != "123" {
			t.Fatalf("column %d did not inherit the selected feed: %+v", index, c)
		}
		if c.id != index {
			t.Fatalf("column %d id=%d, want %d", index, c.id, index)
		}
	}
}

func TestInitSchedulesAFirstPageForEveryColumn(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(3, FeedFollowing, "", "")

	msg := m.Init()()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init returned %T, want tea.BatchMsg", msg)
	}
	if len(batch) != 5 {
		t.Fatalf("Init scheduled %d commands, want spinner + 3 fetches + clock", len(batch))
	}
}

func TestTwoColumnsRenderSideBySideWithoutOverflowAt100Cols(t *testing.T) {
	m := modelWithNamedColumns("Alice", "Bob")
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})

	view := m.View()
	if !lineContainsAll(view, "Alice", "Bob") {
		t.Fatalf("columns did not render side by side:\n%s", view)
	}
	if width := maxLineWidth(view); width > 98 {
		t.Fatalf("view width=%d fills the two-cell safety reserve:\n%s", width, view)
	}
}

func TestNarrowTerminalCollapsesToOneColumnAndNotesHiddenColumns(t *testing.T) {
	m := modelWithNamedColumns("Alice", "Bob")
	m = update(t, m, tea.WindowSizeMsg{Width: 42, Height: 15})

	view := m.View()
	if !strings.Contains(view, "Alice") || strings.Contains(view, "Bob") {
		t.Fatalf("42-column view did not collapse to only the focused column:\n%s", view)
	}
	if !strings.Contains(view, "+1 more (widen terminal)") {
		t.Fatalf("collapsed view omitted its hidden-column note:\n%s", view)
	}
	if width := maxLineWidth(view); width > 40 {
		t.Fatalf("collapsed view width=%d fills the two-cell safety reserve:\n%s", width, view)
	}
}

func TestFocusCycleMovesActionTargetBetweenColumns(t *testing.T) {
	m := modelWithNamedColumns("Alice", "Bob")
	m.columns[0].posts[0].ID = "first"
	m.columns[1].posts[0].ID = "second"
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})

	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != 1 {
		t.Fatalf("tab focused column %d, want 1", m.focus)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if m.columns[0].posts[0].Liked {
		t.Fatal("liking in column 2 changed column 1's different post")
	}
	if !m.columns[1].posts[0].Liked {
		t.Fatal("liking in column 2 did not update its selected post")
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus != 0 {
		t.Fatalf("shift+tab focused column %d, want 0", m.focus)
	}
}

func TestLikeFansOutToDuplicatePostAcrossColumns(t *testing.T) {
	m := modelWithNamedColumns("Alice", "Bob")
	m.columns[0].posts[0].ID = "shared"
	m.columns[1].posts[0].ID = "shared"

	m.applyLike("shared", true)

	for index := range m.columns {
		post := m.columns[index].posts[0]
		if !post.Liked || post.LikeCount != 1 {
			t.Fatalf("column %d duplicate was not updated: %+v", index, post)
		}
	}
}

func TestFocusedColumnStaysVisibleWhenWindowSlides(t *testing.T) {
	m := modelWithNamedColumns("Alice", "Bob", "Carol", "Dave")
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	if view := m.View(); !strings.Contains(view, "Alice") || strings.Contains(view, "Dave") {
		t.Fatalf("initial three-column window is wrong:\n%s", view)
	}

	for range 3 {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	}
	view := m.View()
	if m.focus != 3 || strings.Contains(view, "Alice") || !strings.Contains(view, "Dave") {
		t.Fatalf("window did not slide to focused column 4:\n%s", view)
	}
}

func TestTwoColumnViewIsFixedHeightAndDoesNotDuplicateRows(t *testing.T) {
	m := modelWithNamedColumns("Alice", "Alice")
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})

	view := m.View()
	if strings.Count(view, "Alice") != 2 {
		t.Fatalf("Alice should appear once in each column:\n%s", view)
	}
	if strings.Count(view, "♡") != 2 {
		t.Fatalf("each column should render one action line:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines != 24 {
		t.Fatalf("two-column view has %d lines, want 24", lines)
	}
}

func TestThreadOverlayUsesFullTerminalWidthWithMultipleColumns(t *testing.T) {
	m := modelWithNamedColumns("Alice", "Bob")
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadPosts = []api.ConversationPost{{
		TimelinePost: api.TimelinePost{ID: "root", AuthorName: "Root", Handle: "root", Text: "full-width thread"},
	}}
	m.cur().selected = 0
	m.resize()

	if got := m.cur().viewport.Width; got != 76 {
		t.Fatalf("thread viewport width=%d, want full-screen width 76", got)
	}
	if view := m.View(); !strings.Contains(view, "full-width thread") {
		t.Fatalf("thread overlay did not render its content:\n%s", view)
	}
}

func modelWithNamedColumns(names ...string) Model {
	m := NewWithImageMode("off")
	m.configureColumns(len(names), FeedForYou, "", "")
	for index, name := range names {
		m.columns[index].loading = false
		m.columns[index].posts = []api.TimelinePost{{
			ID:         strings.ToLower(name),
			AuthorName: name,
			Handle:     strings.ToLower(name),
			Text:       "a post in this column",
		}}
	}
	return m
}

func lineContainsAll(value string, parts ...string) bool {
	for _, line := range strings.Split(value, "\n") {
		found := true
		for _, part := range parts {
			if !strings.Contains(line, part) {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}
