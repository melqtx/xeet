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

func TestExpiredSessionOffersReconnectAction(t *testing.T) {
	m := New()
	m.loading = false
	m.err = api.ErrSessionExpired
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
	m.loading = false
	m.err = &api.ConnectionError{Kind: "offline", Err: errors.New("offline")}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if m.action.Kind == ActionAuthenticate || cmd != nil {
		t.Fatal("a unexpectedly started reconnect flow for a network error")
	}
}

func TestAltTextPanelListsEveryImageWithoutRenderer(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "1", Text: "photos", AuthorName: "Alice", Handle: "alice", Media: []api.TimelineMedia{
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
	m.loading = false
	m.height = 12
	m.posts = []api.TimelinePost{{ID: "1", Media: []api.TimelineMedia{{
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
	m.loading = false
	m.altText = true
	m.posts = []api.TimelinePost{{ID: "1", Liked: true, LikeCount: 1, Media: []api.TimelineMedia{{}}}}
	m.liking["1"] = true
	next, _ := m.Update(likeMsg{id: "1", liked: true, err: errors.New("rejected")})
	m = next.(Model)
	if !m.altText {
		t.Fatal("background result unexpectedly closed alt text")
	}
	if m.liking["1"] || m.posts[0].Liked || m.posts[0].LikeCount != 0 {
		t.Fatalf("like result was dropped: liking=%v post=%+v", m.liking, m.posts[0])
	}
}

func TestHelpProcessesBackgroundResults(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.help = true
	m.posts = []api.TimelinePost{{ID: "1", Liked: true, LikeCount: 1}}
	m.liking["1"] = true
	next, _ := m.Update(likeMsg{id: "1", liked: true, err: errors.New("rejected")})
	m = next.(Model)
	if !m.help {
		t.Fatal("background result unexpectedly closed help")
	}
	if m.liking["1"] || m.posts[0].Liked || m.posts[0].LikeCount != 0 {
		t.Fatalf("like result was dropped: liking=%v post=%+v", m.liking, m.posts[0])
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

func TestEnterOpensRepliesAndEscRestoresFeed(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.posts = []api.TimelinePost{
		{ID: "first", Handle: "one", Text: "first"},
		{ID: "root", Handle: "alice", Text: "root"},
	}
	m.selected = 1
	m.syncViewport()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || m.mode != modeThread || m.threadRootID != "root" || m.feedSelected != 1 {
		t.Fatalf("thread did not open: mode=%v root=%q feedSelected=%d", m.mode, m.threadRootID, m.feedSelected)
	}
	m = update(t, m, threadMsg{rootID: "root", seq: 1, page: &api.ConversationPage{Posts: []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Handle: "alice", Text: "root"}},
		{TimelinePost: api.TimelinePost{ID: "reply", Handle: "bob", Text: "hello", InReplyToID: "root"}, Depth: 1},
	}}})
	if m.threadLoading || len(m.threadPosts) != 2 || !strings.Contains(m.View(), "↳ reply") {
		t.Fatalf("thread did not render: loading=%v posts=%+v", m.threadLoading, m.threadPosts)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeFeed || m.selected != 1 || m.posts[m.selected].ID != "root" {
		t.Fatalf("feed position was not restored: mode=%v selected=%d", m.mode, m.selected)
	}
}

func TestThreadIgnoresStaleConversationAndKeepsFeedUpdatesAlive(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "root", Text: "root"}}
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadPosts = []api.ConversationPost{{TimelinePost: m.posts[0]}}
	m.threadLoading = true
	m = update(t, m, threadMsg{rootID: "old", page: &api.ConversationPage{Posts: []api.ConversationPost{{
		TimelinePost: api.TimelinePost{ID: "wrong", Text: "wrong"},
	}}}})
	if len(m.threadPosts) != 1 || m.threadPosts[0].ID != "root" || !m.threadLoading {
		t.Fatalf("stale thread result was applied: %+v", m.threadPosts)
	}
	m.loadingMore = true
	m = update(t, m, pageMsg{more: true, page: &api.TimelinePage{Posts: []api.TimelinePost{{ID: "next", Text: "next"}}}})
	if m.loadingMore || len(m.posts) != 2 || m.mode != modeThread || m.selected != 0 {
		t.Fatalf("feed update was dropped in thread mode: loadingMore=%v posts=%d selected=%d", m.loadingMore, len(m.posts), m.selected)
	}
}

func TestThreadContinuationResolvesParentFromEarlierPage(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadSeq = 1
	m.threadMore = true
	m.threadPosts = []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}},
		{TimelinePost: api.TimelinePost{ID: "parent", Text: "parent", InReplyToID: "root"}, Depth: 1},
	}
	m = update(t, m, threadMsg{rootID: "root", seq: 1, more: true, page: &api.ConversationPage{
		Unresolved: []api.TimelinePost{{ID: "nested", Text: "nested", InReplyToID: "parent"}},
	}})
	if len(m.threadPosts) != 3 || m.threadPosts[2].ID != "nested" || m.threadPosts[2].Depth != 2 {
		t.Fatalf("continuation was not resolved: %+v", m.threadPosts)
	}
}

func TestThreadResultCompletesWhileReplyEditorIsOpen(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadSeq = 1
	m.threadLoading = true
	m.threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = update(t, m, threadMsg{rootID: "root", seq: 1, page: &api.ConversationPage{Posts: []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}},
		{TimelinePost: api.TimelinePost{ID: "reply", Text: "reply"}, Depth: 1},
	}}})
	if m.mode != modeReply || m.threadLoading || len(m.threadPosts) != 2 {
		t.Fatalf("background thread result was dropped: mode=%v loading=%v posts=%d", m.mode, m.threadLoading, len(m.threadPosts))
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeThread || len(m.threadPosts) != 2 {
		t.Fatalf("cancel did not reveal loaded thread: mode=%v posts=%d", m.mode, len(m.threadPosts))
	}
}

func TestFeedResultDuringThreadReplyPreservesBothSelections(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "a", Text: "a"}, {ID: "root", Text: "root"}}
	m.feedSelected = 1
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadPosts = []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}},
		{TimelinePost: api.TimelinePost{ID: "reply", Text: "reply"}, Depth: 1},
	}
	m.selected = 1
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = update(t, m, pageMsg{page: &api.TimelinePage{Posts: []api.TimelinePost{{ID: "new", Text: "new"}}}})
	if m.mode != modeReply || m.selected != 1 || m.feedSelected != 2 {
		t.Fatalf("background feed result corrupted selections: mode=%v thread=%d feed=%d", m.mode, m.selected, m.feedSelected)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeThread || m.selected != 1 || m.threadPosts[m.selected].ID != "reply" {
		t.Fatalf("thread selection was not restored: mode=%v selected=%d", m.mode, m.selected)
	}
}

func TestThreadIgnoresOlderRequestForSameRoot(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadSeq = 2
	m.threadLoading = true
	m.threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "new root"}}}
	m = update(t, m, threadMsg{rootID: "root", seq: 1, page: &api.ConversationPage{Posts: []api.ConversationPost{{
		TimelinePost: api.TimelinePost{ID: "root", Text: "stale root"},
	}}}})
	if !m.threadLoading || m.threadPosts[0].Text != "new root" {
		t.Fatalf("older same-root request was applied: loading=%v post=%+v", m.threadLoading, m.threadPosts[0])
	}
}

func TestThreadErrorIsVisibleWithSeededRoot(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadSeq = 1
	m.threadLoading = true
	m.threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m = update(t, m, threadMsg{rootID: "root", seq: 1, err: errors.New("discovery failed badly")})
	view := m.View()
	if !strings.Contains(view, "discovery failed badly") || !strings.Contains(view, "R retry") {
		t.Fatalf("thread error not shown despite seeded root:\n%s", view)
	}
}

func TestThreadSessionExpiryOffersReconnect(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadSeq = 1
	m.threadLoading = true
	m.threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
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
	m.loading = false
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadErr = api.ErrSessionExpired
	m.threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeFeed || !errors.Is(m.err, api.ErrSessionExpired) {
		t.Fatalf("esc dropped the expired session: mode=%v err=%v", m.mode, m.err)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if m.action.Kind != ActionAuthenticate || cmd == nil {
		t.Fatal("feed reconnect was unreachable after leaving the thread")
	}
}

func TestThreadEscKeepsOrdinaryErrorsOutOfFeed(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadErr = errors.New("replies timed out")
	m.threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "root", Text: "root"}}}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeFeed || m.err != nil {
		t.Fatalf("thread-only error leaked into the feed: err=%v", m.err)
	}
}

func TestThreadRefreshKeepsRootWhenPageOmitsIt(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadSeq = 1
	m.threadLoading = true
	m.threadPosts = []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Text: "the focal post"}},
		{TimelinePost: api.TimelinePost{ID: "old", Text: "old reply", InReplyToID: "root"}, Depth: 1},
	}
	m = update(t, m, threadMsg{rootID: "root", seq: 1, page: &api.ConversationPage{Posts: []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "reply", Text: "only reply", InReplyToID: "root"}, Depth: 1},
	}}})
	if len(m.threadPosts) != 2 || m.threadPosts[0].ID != "root" || m.threadPosts[0].Depth != 0 {
		t.Fatalf("refresh dropped the focal post: %+v", m.threadPosts)
	}
	if m.threadPosts[1].ID != "reply" {
		t.Fatalf("refresh lost the new reply: %+v", m.threadPosts)
	}
}

func TestThreadReplyTargetsSelectedReplyAndReturnsToThread(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.mode = modeThread
	m.threadRootID = "root"
	m.threadPosts = []api.ConversationPost{
		{TimelinePost: api.TimelinePost{ID: "root", Handle: "alice", Text: "root"}},
		{TimelinePost: api.TimelinePost{ID: "reply", Handle: "bob", Text: "hello"}, Depth: 1},
	}
	m.selected = 1
	m.syncViewport()
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if m.mode != modeReply || m.replyReturn != modeThread || m.replyPost.ID != "reply" {
		t.Fatalf("reply target=%q mode=%v return=%v", m.replyPost.ID, m.mode, m.replyReturn)
	}
	m.replyEditor.SetValue("nested answer")
	m = update(t, m, replyResultMsg{id: "new"})
	if m.mode != modeThread || !m.threadLoading || m.toast != "reply sent ♥" {
		t.Fatalf("reply did not return and refresh thread: mode=%v loading=%v toast=%q", m.mode, m.threadLoading, m.toast)
	}
}

func TestReadKeyExpandsTruncatedPost(t *testing.T) {
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
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if !m.expanded {
		t.Fatal("e did not expand the post")
	}
	content, _, _ := m.renderFeedContent()
	if !strings.Contains(content, "ENDMARKER") {
		t.Fatal("expanded post is still truncated")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if m.expanded {
		t.Fatal("e did not collapse the post")
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
