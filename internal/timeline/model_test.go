package timeline

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/api"

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

func TestHelpAllowsImmediateQuit(t *testing.T) {
	m := New()
	m.help = true
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q in help did not request quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q in help returned a non-quit command")
	}
}

func TestPageLoadsAndNavigation(t *testing.T) {
	m := New()
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: posts(10), Cursor: "next"}})
	if m.cur().loading || len(m.cur().posts) != 10 {
		t.Fatalf("page did not load: %+v", m)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cur().selected != 1 {
		t.Fatalf("selected=%d", m.cur().selected)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.cur().selected != 0 {
		t.Fatalf("selected=%d", m.cur().selected)
	}
}

func TestLikeIsOptimisticAndRollsBack(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().posts = []api.TimelinePost{{ID: "1", Text: "post", LikeCount: 4}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = next.(Model)
	if cmd == nil || !m.cur().posts[0].Liked || m.cur().posts[0].LikeCount != 5 {
		t.Fatalf("like was not optimistic: %+v", m.cur().posts[0])
	}
	m = update(t, m, likeMsg{id: "1", liked: true, err: errors.New("nope")})
	if m.cur().posts[0].Liked || m.cur().posts[0].LikeCount != 4 {
		t.Fatalf("failed like did not roll back: %+v", m.cur().posts[0])
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

func TestExpiredSessionOffersReconnectAction(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().err = api.ErrSessionExpired
	if view := m.View(); !strings.Contains(view, "a reconnect") || !strings.Contains(view, "xeet auth") {
		t.Fatalf("expired-session guidance missing: %s", view)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if m.action.Kind != ActionAuthenticate || cmd == nil {
		t.Fatal("a did not start reconnect flow")
	}
}

func TestReconnectKeyIgnoredForOtherErrors(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().err = &api.ConnectionError{Kind: "offline", Err: errors.New("offline")}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if m.action.Kind == ActionAuthenticate || cmd != nil {
		t.Fatal("a unexpectedly started reconnect flow for a network error")
	}
}

func TestAltTextPanelListsEveryImageWithoutRenderer(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.cur().posts = []api.TimelinePost{{ID: "1", Text: "photos", AuthorName: "Alice", Handle: "alice", Media: []api.TimelineMedia{
		{AltText: "a cat on a windowsill"}, {},
	}}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = next.(Model)
	if !m.altText || cmd != nil {
		t.Fatalf("alt panel did not open: altText=%v cmd=%v", m.altText, cmd)
	}
	view := m.View()
	for _, want := range []string{"image 1 of 2", "a cat on a windowsill", "image 2 of 2", "No alt text was provided"} {
		if !strings.Contains(view, want) {
			t.Fatalf("alt panel missing %q: %s", want, view)
		}
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.altText {
		t.Fatal("alt panel did not close")
	}
}

func TestAltTextPanelScrollsLongDescriptions(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.height = 12
	m.cur().posts = []api.TimelinePost{{ID: "1", Media: []api.TimelineMedia{{
		AltText: strings.Repeat("a long accessible description ", 30),
	}}}}
	m.altText = true
	before := m.View()
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.altTextScroll != 1 {
		t.Fatalf("scroll=%d, want 1", m.altTextScroll)
	}
	if after := m.View(); after == before || !strings.Contains(after, "↑/↓ scroll") {
		t.Fatalf("long alt text did not scroll:\n%s", after)
	}
}

func TestAltTextPanelProcessesBackgroundResults(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.altText = true
	m.cur().posts = []api.TimelinePost{{ID: "1", Liked: true, LikeCount: 1, Media: []api.TimelineMedia{{}}}}
	m.liking[likeKey("", "1")] = true
	next, _ := m.Update(likeMsg{id: "1", liked: true, err: errors.New("rejected")})
	m = next.(Model)
	if !m.altText {
		t.Fatal("background result unexpectedly closed alt text")
	}
	if m.liking[likeKey("", "1")] || m.cur().posts[0].Liked || m.cur().posts[0].LikeCount != 0 {
		t.Fatalf("like result was dropped: liking=%v post=%+v", m.liking, m.cur().posts[0])
	}
}

func TestHelpProcessesBackgroundResults(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.help = true
	m.cur().posts = []api.TimelinePost{{ID: "1", Liked: true, LikeCount: 1}}
	m.liking[likeKey("", "1")] = true
	next, _ := m.Update(likeMsg{id: "1", liked: true, err: errors.New("rejected")})
	m = next.(Model)
	if !m.help {
		t.Fatal("background result unexpectedly closed help")
	}
	if m.liking[likeKey("", "1")] || m.cur().posts[0].Liked || m.cur().posts[0].LikeCount != 0 {
		t.Fatalf("like result was dropped: liking=%v post=%+v", m.liking, m.cur().posts[0])
	}
}

func TestRefreshKey(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().posts = posts(1)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = next.(Model)
	if !m.cur().refreshing || cmd == nil {
		t.Fatal("R did not refresh in place")
	}
}

func TestReplyOpensInPlaceAndReturnsToFeed(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().posts = []api.TimelinePost{{ID: "123", Handle: "alice", Text: "hello"}}
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
	m.cur().loading = false
	m.cur().posts = []api.TimelinePost{{ID: "123", Handle: "alice", Text: "hello"}}
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
	if len(m.cur().posts) != 8 {
		t.Fatalf("refresh did not merge: %d posts", len(m.cur().posts))
	}
	if m.cur().posts[0].ID != "f" || m.cur().posts[3].ID != "a" {
		t.Fatalf("new posts were not prepended in order: %+v", m.cur().posts[:4])
	}
	if m.cur().posts[m.cur().selected].ID != "b" {
		t.Fatalf("selection moved off post b: selected=%d", m.cur().selected)
	}
	if m.cur().cursor != "first" {
		t.Fatalf("refresh clobbered the pagination cursor: %q", m.cur().cursor)
	}
	if !strings.Contains(m.toast, "3 new") {
		t.Fatalf("no new-posts toast: %q", m.toast)
	}
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: posts(8), Cursor: "third"}})
	if len(m.cur().posts) != 8 || !strings.Contains(m.toast, "caught up") {
		t.Fatalf("no-change refresh misbehaved: %d posts, toast %q", len(m.cur().posts), m.toast)
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

func TestEnterOpensRepliesAndEscRestoresFeed(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.cur().posts = []api.TimelinePost{
		{ID: "first", Handle: "one", Text: "first"},
		{ID: "root", Handle: "alice", Text: "root"},
	}
	m.cur().selected = 1
	m.syncViewport()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || m.mode != modeThread || m.cur().threadRootID != "root" || m.cur().feedSelected != 1 {
		t.Fatalf("thread did not open: mode=%v root=%q feedSelected=%d", m.mode, m.cur().threadRootID, m.cur().feedSelected)
	}
	m = update(t, m, threadMsg{rootID: "root", seq: 1, page: &api.ConversationPage{Posts: []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Handle: "alice", Text: "root"}},
		{TimelinePost: api.TimelinePost{ID: "reply", Handle: "bob", Text: "hello", InReplyToID: "root"}, Depth: 1},
	}}})
	view := m.View()
	if m.cur().threadLoading || len(m.cur().threadPosts) != 2 || !strings.Contains(view, "@bob") {
		t.Fatalf("thread did not render: loading=%v posts=%+v", m.cur().threadLoading, m.cur().threadPosts)
	}
	if !strings.Contains(view, "replies to @alice") {
		t.Fatalf("thread header does not name the focal author:\n%s", view)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeFeed || m.cur().selected != 1 || m.cur().posts[m.cur().selected].ID != "root" {
		t.Fatalf("feed position was not restored: mode=%v selected=%d", m.mode, m.cur().selected)
	}
}

func TestThreadIgnoresStaleConversationAndKeepsFeedUpdatesAlive(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.cur().posts = []api.TimelinePost{{ID: "root", Text: "root"}}
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadPosts = []api.ConversationPost{{TimelinePost: m.cur().posts[0]}}
	m.cur().threadLoading = true
	m = update(t, m, threadMsg{rootID: "old", page: &api.ConversationPage{Posts: []api.ConversationPost{{
		TimelinePost: api.TimelinePost{ID: "wrong", Text: "wrong"},
	}}}})
	if len(m.cur().threadPosts) != 1 || m.cur().threadPosts[0].ID != "root" || !m.cur().threadLoading {
		t.Fatalf("stale thread result was applied: %+v", m.cur().threadPosts)
	}
	m.cur().loadingMore = true
	m = update(t, m, pageMsg{more: true, page: &api.TimelinePage{Posts: []api.TimelinePost{{ID: "next", Text: "next"}}}})
	if m.cur().loadingMore || len(m.cur().posts) != 2 || m.mode != modeThread || m.cur().selected != 0 {
		t.Fatalf("feed update was dropped in thread mode: loadingMore=%v posts=%d selected=%d", m.cur().loadingMore, len(m.cur().posts), m.cur().selected)
	}
}

func TestThreadContinuationResolvesParentFromEarlierPage(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadSeq = 1
	m.cur().threadMore = true
	m.cur().threadPosts = []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}},
		{TimelinePost: api.TimelinePost{ID: "parent", Text: "parent", InReplyToID: "root"}, Depth: 1},
	}
	m = update(t, m, threadMsg{rootID: "root", seq: 1, more: true, page: &api.ConversationPage{
		Unresolved: []api.TimelinePost{{ID: "nested", Text: "nested", InReplyToID: "parent"}},
	}})
	if len(m.cur().threadPosts) != 3 || m.cur().threadPosts[2].ID != "nested" || m.cur().threadPosts[2].Depth != 2 {
		t.Fatalf("continuation was not resolved: %+v", m.cur().threadPosts)
	}
}

func TestThreadResultCompletesWhileReplyEditorIsOpen(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadSeq = 1
	m.cur().threadLoading = true
	m.cur().threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = update(t, m, threadMsg{rootID: "root", seq: 1, page: &api.ConversationPage{Posts: []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}},
		{TimelinePost: api.TimelinePost{ID: "reply", Text: "reply"}, Depth: 1},
	}}})
	if m.mode != modeReply || m.cur().threadLoading || len(m.cur().threadPosts) != 2 {
		t.Fatalf("background thread result was dropped: mode=%v loading=%v posts=%d", m.mode, m.cur().threadLoading, len(m.cur().threadPosts))
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeThread || len(m.cur().threadPosts) != 2 {
		t.Fatalf("cancel did not reveal loaded thread: mode=%v posts=%d", m.mode, len(m.cur().threadPosts))
	}
}

func TestFeedResultDuringThreadReplyPreservesBothSelections(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.cur().posts = []api.TimelinePost{{ID: "a", Text: "a"}, {ID: "root", Text: "root"}}
	m.cur().feedSelected = 1
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadPosts = []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}},
		{TimelinePost: api.TimelinePost{ID: "reply", Text: "reply"}, Depth: 1},
	}
	m.cur().selected = 1
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: []api.TimelinePost{{ID: "new", Text: "new"}}}})
	if m.mode != modeReply || m.cur().selected != 1 || m.cur().feedSelected != 2 {
		t.Fatalf("background feed result corrupted selections: mode=%v thread=%d feed=%d", m.mode, m.cur().selected, m.cur().feedSelected)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeThread || m.cur().selected != 1 || m.cur().threadPosts[m.cur().selected].ID != "reply" {
		t.Fatalf("thread selection was not restored: mode=%v selected=%d", m.mode, m.cur().selected)
	}
}

func TestThreadIgnoresOlderRequestForSameRoot(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadSeq = 2
	m.cur().threadLoading = true
	m.cur().threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "new root"}}}
	m = update(t, m, threadMsg{rootID: "root", seq: 1, page: &api.ConversationPage{Posts: []api.ConversationPost{{
		TimelinePost: api.TimelinePost{ID: "root", Text: "stale root"},
	}}}})
	if !m.cur().threadLoading || m.cur().threadPosts[0].Text != "new root" {
		t.Fatalf("older same-root request was applied: loading=%v post=%+v", m.cur().threadLoading, m.cur().threadPosts[0])
	}
}

func TestThreadErrorIsVisibleWithSeededRoot(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadSeq = 1
	m.cur().threadLoading = true
	m.cur().threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m = update(t, m, threadMsg{rootID: "root", seq: 1, err: errors.New("discovery failed badly")})
	view := m.View()
	if !strings.Contains(view, "discovery failed badly") || !strings.Contains(view, "R retry") {
		t.Fatalf("thread error not shown despite seeded root:\n%s", view)
	}
}

func TestThreadSessionExpiryOffersReconnect(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadSeq = 1
	m.cur().threadLoading = true
	m.cur().threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m = update(t, m, threadMsg{rootID: "root", seq: 1, err: api.ErrSessionExpired})
	if !strings.Contains(m.View(), "a reconnect") {
		t.Fatalf("session-expired thread did not offer reconnect:\n%s", m.View())
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if m.action.Kind != ActionAuthenticate || cmd == nil {
		t.Fatal("a did not start the reconnect flow from thread mode")
	}
}

func TestThreadEscPromotesSessionExpiryToFeed(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadErr = api.ErrSessionExpired
	m.cur().threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeFeed || !errors.Is(m.cur().err, api.ErrSessionExpired) {
		t.Fatalf("esc dropped the expired session: mode=%v err=%v", m.mode, m.cur().err)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if m.action.Kind != ActionAuthenticate || cmd == nil {
		t.Fatal("feed reconnect was unreachable after leaving the thread")
	}
}

func TestThreadEscKeepsOrdinaryErrorsOutOfFeed(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadErr = errors.New("replies timed out")
	m.cur().threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeFeed || m.cur().err != nil {
		t.Fatalf("thread-only error leaked into the feed: err=%v", m.cur().err)
	}
}

func TestThreadRefreshKeepsRootWhenPageOmitsIt(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadSeq = 1
	m.cur().threadLoading = true
	m.cur().threadPosts = []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Text: "the focal post"}},
		{TimelinePost: api.TimelinePost{ID: "old", Text: "old reply", InReplyToID: "root"}, Depth: 1},
	}
	m = update(t, m, threadMsg{rootID: "root", seq: 1, page: &api.ConversationPage{Posts: []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "reply", Text: "only reply", InReplyToID: "root"}, Depth: 1},
	}}})
	if len(m.cur().threadPosts) != 2 || m.cur().threadPosts[0].ID != "root" || m.cur().threadPosts[0].Depth != 0 {
		t.Fatalf("refresh dropped the focal post: %+v", m.cur().threadPosts)
	}
	if m.cur().threadPosts[1].ID != "reply" {
		t.Fatalf("refresh lost the new reply: %+v", m.cur().threadPosts)
	}
}

func TestThreadReplyTargetsSelectedReplyAndReturnsToThread(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.mode = modeThread
	m.cur().threadRootID = "root"
	m.cur().threadPosts = []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Handle: "alice", Text: "root"}},
		{TimelinePost: api.TimelinePost{ID: "reply", Handle: "bob", Text: "hello"}, Depth: 1},
	}
	m.cur().selected = 1
	m.syncViewport()
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if m.mode != modeReply || m.replyReturn != modeThread || m.replyPost.ID != "reply" {
		t.Fatalf("reply target=%q mode=%v return=%v", m.replyPost.ID, m.mode, m.replyReturn)
	}
	m.replyEditor.SetValue("nested answer")
	m = update(t, m, replyResultMsg{id: "new"})
	if m.mode != modeThread || !m.cur().threadLoading || m.toast != "reply sent ♥" {
		t.Fatalf("reply did not return and refresh thread: mode=%v loading=%v toast=%q", m.mode, m.cur().threadLoading, m.toast)
	}
}

func TestReadKeyExpandsTruncatedPost(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().posts = []api.TimelinePost{{
		ID: "1", Handle: "alice",
		Text: strings.Repeat("word ", 60) + "ENDMARKER",
	}}
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if strings.Contains(m.cur().viewport.View(), "ENDMARKER") {
		t.Fatal("long post was not truncated")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if !m.cur().expanded {
		t.Fatal("e did not expand the post")
	}
	content, _, _ := m.renderFeedContent()
	if !strings.Contains(content, "ENDMARKER") {
		t.Fatal("expanded post is still truncated")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if m.cur().expanded {
		t.Fatal("e did not collapse the post")
	}
}

func TestHalfPageJumpAndScrolloff(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().posts = posts(30)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.cur().selected != 5 {
		t.Fatalf("ctrl+d selected=%d", m.cur().selected)
	}
	for i := 0; i < 4; i++ {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
	}
	if m.cur().selected != 25 {
		t.Fatalf("selected=%d", m.cur().selected)
	}
	top := m.cur().viewport.YOffset
	maxTop := m.cur().ends[len(m.cur().ends)-1] + 1 - m.cur().viewport.Height
	if end := m.cur().ends[m.cur().selected]; end >= top+m.cur().viewport.Height {
		t.Fatalf("selection scrolled out of view: end=%d top=%d", end, top)
	}
	if end := m.cur().ends[m.cur().selected]; end+2 >= top+m.cur().viewport.Height && top != maxTop {
		t.Fatal("no scroll margin below the selection")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.cur().selected != 20 {
		t.Fatalf("ctrl+u selected=%d", m.cur().selected)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.cur().selected != 0 || m.cur().viewport.YOffset != 0 {
		t.Fatalf("ctrl+u did not clamp: selected=%d offset=%d", m.cur().selected, m.cur().viewport.YOffset)
	}
}

func TestPaginationDeduplicates(t *testing.T) {
	m := New()
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: posts(2), Cursor: "one"}})
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: posts(3), Cursor: "two"}, more: true})
	if len(m.cur().posts) != 3 || m.cur().cursor != "two" {
		t.Fatalf("unexpected posts: %+v", m.cur().posts)
	}
}

func TestPageForRemovedColumnIsDropped(t *testing.T) {
	m := New()
	before := m

	m = update(t, m, pageMsg{
		colID: 99,
		page:  &api.TimelinePage{Posts: posts(2), Cursor: "removed"},
	})

	if !reflect.DeepEqual(m, before) {
		t.Fatal("page for a removed column changed the model")
	}
}

func TestPageRoutedToNonFocusedColumnDoesNotTouchFocusedColumn(t *testing.T) {
	m := New()
	m.columns = []column{
		{
			id:       7,
			feedSeq:  4,
			posts:    []api.TimelinePost{{ID: "focused", Text: "focused"}},
			cursor:   "focused-cursor",
			selected: 0,
			viewport: m.cur().viewport,
		},
		{
			id:          8,
			feedSeq:     4,
			posts:       []api.TimelinePost{{ID: "background", Text: "background"}},
			cursor:      "background-cursor",
			loadingMore: true,
		},
	}
	m.focus = 0
	m.nextColID = 9
	focusedBefore := m.columns[0]

	m = update(t, m, pageMsg{
		colID: 8,
		seq:   4,
		more:  true,
		page: &api.TimelinePage{
			Posts:  []api.TimelinePost{{ID: "routed", Text: "routed"}},
			Cursor: "routed-cursor",
		},
	})

	if !reflect.DeepEqual(m.columns[0], focusedBefore) {
		t.Fatal("page for a non-focused column changed the focused column")
	}
	if got := m.columns[1]; len(got.posts) != 2 || got.posts[1].ID != "routed" ||
		got.cursor != "routed-cursor" || got.loadingMore {
		t.Fatalf("page was not applied to its originating column: %+v", got)
	}
}

func TestViewIsFixedHeightAndDoesNotDuplicateRows(t *testing.T) {
	for _, size := range []struct{ width, height int }{{40, 15}, {80, 24}, {100, 30}, {160, 50}} {
		m := New()
		m.cur().loading = false
		m.cur().posts = []api.TimelinePost{
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

func TestRequestContextCarriesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := New()
	m.ctx = ctx
	if m.requestContext().Err() != nil {
		t.Fatal("live context reported as cancelled")
	}
	cancel()
	if !errors.Is(m.requestContext().Err(), context.Canceled) {
		t.Fatal("cancelling the parent did not reach the model's request context")
	}

	// A zero-value Model (built directly by a test) must still be usable.
	if (Model{}).requestContext() == nil {
		t.Fatal("zero-value model has no request context")
	}
}
