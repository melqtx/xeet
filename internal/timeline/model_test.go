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

func TestRejectedReplyOffersBrowserFallback(t *testing.T) {
	m := New()
	m.mode = modeReply
	m.replyPost = api.TimelinePost{ID: "123", Handle: "alice"}
	m.replyEditor.SetValue("no")
	m.replyErr = &api.PostingRestrictedError{}

	if !strings.Contains(m.View(), "press b to try in X") {
		t.Fatal("reply browser fallback was not shown")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = next.(Model)
	if cmd == nil || m.replyEditor.Value() != "no" || m.replyPost.ID != "123" {
		t.Fatalf("cmd=%v text=%q post=%q", cmd, m.replyEditor.Value(), m.replyPost.ID)
	}
}

func TestAutomationRejectedReplyOffersBrowserFallback(t *testing.T) {
	m := New()
	m.mode = modeReply
	m.replyPost = api.TimelinePost{ID: "123", Handle: "alice"}
	m.replyEditor.SetValue("keep these exact words")
	m.replyErr = &api.AutomationBlockedError{}

	view := m.View()
	if !strings.Contains(view, "suspected automation; press b to try it in X") {
		t.Fatalf("automation fallback was not shown: %s", view)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = next.(Model)
	if cmd == nil || m.replyEditor.Value() != "keep these exact words" {
		t.Fatalf("cmd=%v text=%q", cmd, m.replyEditor.Value())
	}
}

func TestReplyBrowserSuccessKeepsDraft(t *testing.T) {
	m := New()
	m.mode = modeReply
	m.replyEditor.SetValue("no")
	m.replyErr = &api.PostingRestrictedError{}
	m = update(t, m, replyBrowserMsg{})
	if m.replyErr != nil || m.replyNotice != "opened reply in X" || m.replyEditor.Value() != "no" {
		t.Fatalf("err=%v notice=%q text=%q", m.replyErr, m.replyNotice, m.replyEditor.Value())
	}
}

func TestAmbiguousReplyOffersCautiousBrowserFallback(t *testing.T) {
	m := New()
	m.mode = modeReply
	m.replyPost = api.TimelinePost{ID: "123", Handle: "alice"}
	m.replyEditor.SetValue("possibly posted")
	m.replyErr = &api.AmbiguousPostError{}

	if !strings.Contains(m.View(), "check your profile, then press b") {
		t.Fatal("ambiguous reply fallback was not shown")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = next.(Model)
	if cmd == nil || m.replyEditor.Value() != "possibly posted" {
		t.Fatalf("cmd=%v text=%q", cmd, m.replyEditor.Value())
	}
}

func TestRefreshMergesOnTopAndKeepsPosition(t *testing.T) {
	m := New()
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: posts(5), Cursor: "first"}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: posts(8), Cursor: "second"}})
	if len(m.posts) != 8 {
		t.Fatalf("refresh did not merge: %d posts", len(m.posts))
	}
	if m.posts[0].ID != "f" || m.posts[3].ID != "a" {
		t.Fatalf("new posts were not prepended in order: %+v", m.posts[:4])
	}
	if m.posts[m.selected].ID != "b" {
		t.Fatalf("selection moved off post b: selected=%d", m.selected)
	}
	if m.cursor != "first" {
		t.Fatalf("refresh clobbered the pagination cursor: %q", m.cursor)
	}
	if !strings.Contains(m.toast, "3 new") {
		t.Fatalf("no new-posts toast: %q", m.toast)
	}
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: posts(8), Cursor: "third"}})
	if len(m.posts) != 8 || !strings.Contains(m.toast, "caught up") {
		t.Fatalf("no-change refresh misbehaved: %d posts, toast %q", len(m.posts), m.toast)
	}
}

func TestToastExpiresOnlyForLatestSequence(t *testing.T) {
	m := New()
	if cmd := m.showToast("first"); cmd == nil || m.toast != "first" {
		t.Fatal("toast was not shown")
	}
	staleSeq := m.toastSeq
	m.showToast("second")
	m = update(t, m, toastClearMsg{seq: staleSeq})
	if m.toast != "second" {
		t.Fatalf("stale clear removed a newer toast: %q", m.toast)
	}
	m = update(t, m, toastClearMsg{seq: m.toastSeq})
	if m.toast != "" {
		t.Fatalf("toast did not expire: %q", m.toast)
	}
}

func TestEnterExpandsTruncatedPost(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = []api.TimelinePost{{
		ID: "1", Handle: "alice",
		Text: strings.Repeat("word ", 60) + "ENDMARKER",
	}}
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if strings.Contains(m.viewport.View(), "ENDMARKER") {
		t.Fatal("long post was not truncated")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.expanded {
		t.Fatal("enter did not expand the post")
	}
	content, _, _ := m.renderFeedContent()
	if !strings.Contains(content, "ENDMARKER") {
		t.Fatal("expanded post is still truncated")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.expanded {
		t.Fatal("enter did not collapse the post")
	}
}

func TestHalfPageJumpAndScrolloff(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = posts(30)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.selected != 5 {
		t.Fatalf("ctrl+d selected=%d", m.selected)
	}
	for i := 0; i < 4; i++ {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
	}
	if m.selected != 25 {
		t.Fatalf("selected=%d", m.selected)
	}
	top := m.viewport.YOffset
	maxTop := m.ends[len(m.ends)-1] + 1 - m.viewport.Height
	if end := m.ends[m.selected]; end >= top+m.viewport.Height {
		t.Fatalf("selection scrolled out of view: end=%d top=%d", end, top)
	}
	if end := m.ends[m.selected]; end+2 >= top+m.viewport.Height && top != maxTop {
		t.Fatal("no scroll margin below the selection")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.selected != 20 {
		t.Fatalf("ctrl+u selected=%d", m.selected)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.selected != 0 || m.viewport.YOffset != 0 {
		t.Fatalf("ctrl+u did not clamp: selected=%d offset=%d", m.selected, m.viewport.YOffset)
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
		if strings.Count(view, "♡") != 2 {
			t.Fatalf("%dx%d: action lines missing", size.width, size.height)
		}
		if strings.Count(view, "Alice") != 1 || strings.Count(view, "Bob") > 1 {
			t.Fatalf("%dx%d: duplicated timeline rows", size.width, size.height)
		}
		if lines := strings.Count(view, "\n") + 1; lines != size.height {
			t.Fatalf("%dx%d: view has %d lines", size.width, size.height, lines)
		}
	}
}
