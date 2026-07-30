package timeline

import (
	"errors"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/api"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBookmarkKeyTogglesSelectedPostOptimistically(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().posts = posts(3)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("B produced no bookmark request")
	}
	if !m.cur().posts[0].Bookmarked {
		t.Fatal("selected post was not bookmarked optimistically")
	}
	if !m.bookmarking[likeKey(m.columnAccountID(m.cur()), "a")] {
		t.Fatalf("in-flight bookmark not keyed: %v", m.bookmarking)
	}
}

func TestBookmarkFailureRollsBackOptimisticState(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().posts = posts(3)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}})
	m = next.(Model)

	m.applyBookmarkResult(bookmarkMsg{
		id: "a", accountID: m.columnAccountID(m.cur()), bookmarked: true, err: errors.New("boom"),
	})
	if m.cur().posts[0].Bookmarked {
		t.Fatal("failed bookmark was not rolled back")
	}
	if len(m.bookmarking) != 0 {
		t.Fatalf("in-flight marker leaked: %v", m.bookmarking)
	}
	if !strings.Contains(m.toast, "couldn't update bookmark") {
		t.Fatalf("toast = %q", m.toast)
	}
}

func TestBookmarkSuccessKeepsStateAndToasts(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().posts = posts(3)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}})
	m = next.(Model)

	m.applyBookmarkResult(bookmarkMsg{
		id: "a", accountID: m.columnAccountID(m.cur()), bookmarked: true,
	})
	if !m.cur().posts[0].Bookmarked {
		t.Fatal("successful bookmark lost the flag")
	}
	if len(m.bookmarking) != 0 {
		t.Fatalf("in-flight marker leaked: %v", m.bookmarking)
	}
	if !strings.Contains(m.toast, "bookmarked") {
		t.Fatalf("toast = %q", m.toast)
	}
}

func TestBookmarkKeyFromThreadTogglesThreadPost(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.mode = modeThread
	m.cur().threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "t1", Text: "post", Handle: "alice"}}}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("B in thread produced no bookmark request")
	}
	if !m.cur().threadPosts[0].Bookmarked {
		t.Fatal("thread post was not bookmarked optimistically")
	}
}

func TestBookmarkFanOutStaysWithinTheActingAccount(t *testing.T) {
	m := New()
	m.configureColumns([]ColumnSpec{
		{Kind: FeedForYou, AccountID: "42"},
		{Kind: FeedForYou, AccountID: "84"},
		{Kind: FeedFollowing, AccountID: "42"},
	})
	for i := range m.columns {
		m.columns[i].posts = []api.TimelinePost{{ID: "shared"}}
		m.columns[i].threadPosts = []api.ConversationPost{{
			TimelinePost: api.TimelinePost{ID: "shared"},
		}}
	}

	if cmd := m.toggleSelectedBookmark(); cmd == nil {
		t.Fatal("account 42 bookmark did not start")
	}
	if !m.bookmarking[likeKey("42", "shared")] || m.bookmarking[likeKey("84", "shared")] {
		t.Fatalf("in-flight bookmarks are not account-keyed: %v", m.bookmarking)
	}
	for _, index := range []int{0, 2} {
		if !m.columns[index].posts[0].Bookmarked || !m.columns[index].threadPosts[0].Bookmarked {
			t.Fatalf("account 42 column %d did not receive the bookmark", index)
		}
	}
	if m.columns[1].posts[0].Bookmarked || m.columns[1].threadPosts[0].Bookmarked {
		t.Fatal("account 42 bookmark crossed into account 84 state")
	}
}
