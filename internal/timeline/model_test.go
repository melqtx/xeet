package timeline

import (
	"errors"
	"strings"
	"testing"

	"xeet/pkg/api"

	tea "github.com/charmbracelet/bubbletea"
)

func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

func posts(count int) []api.TimelinePost {
	result := make([]api.TimelinePost, count)
	for i := range result {
		result[i] = api.TimelinePost{ID: string(rune('a' + i)), Text: "post", Handle: "cat"}
	}
	return result
}

func TestPageLoadsAndNavigation(t *testing.T) {
	m := New()
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: posts(10), Cursor: "next"}})
	if m.loading || len(m.posts) != 10 {
		t.Fatalf("page did not load: %+v", m)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.selected != 1 {
		t.Fatalf("selected=%d", m.selected)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.selected != 0 {
		t.Fatalf("selected=%d", m.selected)
	}
}

func TestLikeIsOptimisticAndRollsBack(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "1", Text: "post", LikeCount: 4}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = next.(Model)
	if cmd == nil || !m.posts[0].Liked || m.posts[0].LikeCount != 5 {
		t.Fatalf("like was not optimistic: %+v", m.posts[0])
	}
	m = update(t, m, likeMsg{id: "1", liked: true, err: errors.New("nope")})
	if m.posts[0].Liked || m.posts[0].LikeCount != 4 {
		t.Fatalf("failed like did not roll back: %+v", m.posts[0])
	}
}

func TestPostAction(t *testing.T) {
	m := New()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	m = next.(Model)
	if m.action.Kind != ActionCompose || cmd == nil {
		t.Fatal("P did not open the composer")
	}
}

func TestRefreshKey(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = posts(1)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = next.(Model)
	if !m.refreshing || cmd == nil {
		t.Fatal("R did not refresh in place")
	}
}

func TestReplyOpensInPlaceAndReturnsToFeed(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "123", Handle: "alice", Text: "hello"}}
	m.syncViewport()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = next.(Model)
	if cmd == nil || m.mode != modeReply || m.replyPost.ID != "123" {
		t.Fatalf("reply did not open in place")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeFeed {
		t.Fatal("reply did not return to feed")
	}
}

func TestReplyPostsWithoutLeavingProgram(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "123", Handle: "alice", Text: "hello"}}
	m.syncViewport()
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m.replyEditor.SetValue("hey back")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || !m.replyPosting || m.mode != modeReply {
		t.Fatal("reply did not start posting")
	}
	m = update(t, m, replyResultMsg{id: "456"})
	if m.mode != modeFeed || m.toast != "reply sent ♥" {
		t.Fatalf("reply did not return to feed: mode=%v toast=%q", m.mode, m.toast)
	}
}

func TestPaginationDeduplicates(t *testing.T) {
	m := New()
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: posts(2), Cursor: "one"}})
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: posts(3), Cursor: "two"}, more: true})
	if len(m.posts) != 3 || m.cursor != "two" {
		t.Fatalf("unexpected posts: %+v", m.posts)
	}
}

func TestViewIsFixedHeightAndDoesNotDuplicateRows(t *testing.T) {
	for _, size := range []struct{ width, height int }{{40, 15}, {80, 24}, {100, 30}, {160, 50}} {
		m := New()
		m.loading = false
		m.posts = []api.TimelinePost{
			{ID: "1", AuthorName: "Alice", Handle: "alice", Text: "hello from the timeline with enough words to wrap cleanly", MediaCount: 1},
			{ID: "2", AuthorName: "Bob", Handle: "bob", Text: "another post"},
		}
		m = update(t, m, tea.WindowSizeMsg{Width: size.width, Height: size.height})
		view := m.View()
		if !strings.Contains(view, "Like 0") || !strings.Contains(view, "Reply 0") {
			t.Fatalf("%dx%d: action counts missing", size.width, size.height)
		}
		if strings.Count(view, "Alice") != 1 || strings.Count(view, "Bob") > 1 {
			t.Fatalf("%dx%d: duplicated timeline rows", size.width, size.height)
		}
		if lines := strings.Count(view, "\n") + 1; lines != size.height {
			t.Fatalf("%dx%d: view has %d lines", size.width, size.height, lines)
		}
	}
}

func TestImageViewerKey(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = []api.TimelinePost{{
		ID: "123", Text: "photo", MediaCount: 1,
		Media: []api.TimelineMedia{{URL: "https://pbs.twimg.com/media/abc", Type: "photo"}},
	}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = next.(Model)
	if cmd == nil || !strings.Contains(m.toast, "opening") {
		t.Fatalf("image key did not start viewer: toast=%q cmd=%v", m.toast, cmd)
	}
}

func TestImageViewerKeyWithoutMedia(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "123", Text: "text only"}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = next.(Model)
	if cmd != nil || !strings.Contains(m.toast, "no viewable images") {
		t.Fatalf("missing-media feedback: toast=%q cmd=%v", m.toast, cmd)
	}
}
