package timeline

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/api"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func TestListPickerOpensOnCapitalLAndClosesOnEsc(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.feed = FeedFollowing

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	m = next.(Model)

	if cmd == nil || m.mode != modeListPicker || m.listReturn != modeFeed {
		t.Fatalf("list picker did not open from feed: mode=%v return=%v cmd=%v", m.mode, m.listReturn, cmd)
	}
	if !m.listPickerLoading || m.feed != FeedFollowing {
		t.Fatalf("opening picker changed the feed or skipped loading: feed=%v loading=%v", m.feed, m.listPickerLoading)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeFeed || m.feed != FeedFollowing || m.listPickerLoading {
		t.Fatalf("esc did not restore the feed: mode=%v feed=%v loading=%v", m.mode, m.feed, m.listPickerLoading)
	}
}

func TestListPickerSelectionSwitchesFeedAndResetsState(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.feed = FeedBookmarks
	m.feedSeq = 7
	m.posts = []api.TimelinePost{{ID: "old", Text: "old"}}
	m.cursor = "old-cursor"
	m.selected = 1

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	m = update(t, m, listsMsg{lists: []api.ListInfo{
		{ID: "100", Name: "First"},
		{ID: "200", Name: "Second", MemberCount: 12, IsPrivate: true},
	}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if cmd == nil || m.mode != modeFeed || m.feed != FeedList {
		t.Fatalf("list selection did not become a feed: mode=%v feed=%v cmd=%v", m.mode, m.feed, cmd)
	}
	if m.listID != "200" || m.listName != "Second" || m.feedSeq != 8 {
		t.Fatalf("selected list or stale-page sequence is wrong: id=%q name=%q seq=%d", m.listID, m.listName, m.feedSeq)
	}
	if len(m.posts) != 0 || m.cursor != "" || m.selected != 0 || !m.loading {
		t.Fatalf("list feed did not reset for its first page: posts=%d cursor=%q selected=%d loading=%v",
			len(m.posts), m.cursor, m.selected, m.loading)
	}
}

func TestListFeedHeaderShowsListNameWithinWidth(t *testing.T) {
	m := NewWithImageMode("off")
	m.width = 42
	m.loading = false
	m.feed = FeedList
	m.listName = strings.Repeat("very long list name ", 8)

	header := m.header(m.contentWidth())
	if !strings.Contains(header, "List:") {
		t.Fatalf("list header does not identify the feed:\n%s", header)
	}
	if width := maxLineWidth(header); width > m.contentWidth() {
		t.Fatalf("list header width=%d exceeds content width=%d:\n%s", width, m.contentWidth(), header)
	}
}

func TestStalePageFromPreviousFeedIsDroppedAfterListSwitch(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.feed = FeedForYou
	m.feedSeq = 3

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	m = update(t, m, listsMsg{lists: []api.ListInfo{{ID: "100", Name: "Cats"}}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = update(t, m, pageMsg{
		seq:  3,
		page: &api.TimelinePage{Posts: []api.TimelinePost{{ID: "stale", Text: "old feed"}}},
	})

	if m.feed != FeedList || m.feedSeq != 4 || len(m.posts) != 0 {
		t.Fatalf("stale page leaked after list switch: feed=%v seq=%d posts=%v", m.feed, m.feedSeq, m.posts)
	}
}

func TestListPickerRendersLoadingAndErrorStates(t *testing.T) {
	m := NewWithImageMode("off")
	m.width = 60
	m.height = 20
	m.loading = false
	m.beginListPicker()

	if view := m.View(); !strings.Contains(view, "loading lists") {
		t.Fatalf("loading state is not visible:\n%s", view)
	}

	m = update(t, m, listsMsg{err: errors.New("offline")})
	view := m.View()
	for _, want := range []string{"couldn't load lists", "offline", "esc cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("error state is missing %q:\n%s", want, view)
		}
	}
	if m.listPickerLoading {
		t.Fatal("error left the picker loading")
	}
}

func TestListPickerKeepsBackgroundFeedPagesFlowing(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.beginListPicker()

	m = update(t, m, pageMsg{
		page: &api.TimelinePage{Posts: []api.TimelinePost{{ID: "fresh", Text: "fresh"}}},
	})

	if m.mode != modeListPicker || len(m.posts) != 1 || m.posts[0].ID != "fresh" {
		t.Fatalf("background feed page did not flow through picker: mode=%v posts=%v", m.mode, m.posts)
	}
}

func TestListPickerKeepsBackgroundThreadPagesFlowing(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadSeq = 4
	m.threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m.beginListPicker()

	m = update(t, m, threadMsg{
		rootID: "root",
		seq:    4,
		page: &api.ConversationPage{Posts: []api.ConversationPost{{
			TimelinePost: api.TimelinePost{ID: "reply", InReplyToID: "root", Text: "reply"},
			Depth:        1,
		}}},
	})

	if m.mode != modeListPicker || len(m.threadPosts) != 2 {
		t.Fatalf("background thread page did not flow through picker: mode=%v posts=%v", m.mode, m.threadPosts)
	}
}

func TestListPickerKeepsLikePreviewAndSpinnerResultsFlowing(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "post", Liked: true}}
	m.liking["post"] = true
	m.beginListPicker()

	m = update(t, m, likeMsg{id: "post", liked: true})
	m = update(t, m, previewMsg{postID: "post", content: "preview"})
	next, cmd := m.Update(spinner.TickMsg{Time: time.Now()})
	m = next.(Model)

	if m.liking["post"] {
		t.Fatal("like result did not settle while picker was open")
	}
	if got := m.previews["post"].content; got != "preview" {
		t.Fatalf("preview result did not store while picker was open: %q", got)
	}
	if cmd == nil || m.mode != modeListPicker {
		t.Fatalf("spinner did not keep ticking in picker: mode=%v cmd=%v", m.mode, cmd)
	}
}
