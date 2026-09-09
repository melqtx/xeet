package timeline

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/melqtx/xeet/internal/media"
	"github.com/melqtx/xeet/pkg/api"
	"strings"
	"testing"
)

func TestQuoteNavigationReturnsToParentThread(t *testing.T) {
	m := NewWithImageMode("off")
	m.width = 80
	m.height = 24
	m.mode = modeThread
	m.threadRootID = "parent"
	m.threadSeq = 10
	m.threadLoading = true
	m.selected = 1
	m.expanded = true
	m.threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "parent"}}, {TimelinePost: api.TimelinePost{ID: "outer", Quote: &api.TimelinePost{ID: "123", Handle: "alice", Quote: &api.TimelinePost{ID: "456"}}}}}
	m.resize()
	offset := m.viewport.YOffset
	next, _ := m.openSelectedQuote()
	m = next.(Model)
	if m.threadRootID != "123" || len(m.quoteStack) != 1 {
		t.Fatal("quote not opened")
	}
	childSeq := m.threadSeq
	next, _ = m.openSelectedQuote()
	m = next.(Model)
	if m.threadRootID != "456" {
		t.Fatal("nested quote not opened")
	}
	next, _ = m.leaveThread()
	m = next.(Model)
	if m.threadRootID != "123" || m.threadSeq != childSeq {
		t.Fatal("nested quote back failed")
	}
	next, _ = m.leaveThread()
	m = next.(Model)
	if m.threadRootID != "parent" || m.selected != 1 || !m.expanded || m.viewport.YOffset != offset || !m.threadLoading {
		t.Fatal("parent state lost")
	}
	next, _ = m.applyThreadPage(threadMsg{rootID: "123", seq: childSeq, page: &api.ConversationPage{}}, true)
	m = next.(Model)
	if m.threadRootID != "parent" {
		t.Fatal("late quote response changed parent")
	}
}

func TestQuoteNavigationFromNotificationsUsesQuotedRoot(t *testing.T) {
	m := NewWithImageMode("off")
	m.mode = modeNotifications
	m.notifications = []api.Notification{{Post: api.TimelinePost{ID: "outer", Quote: &api.TimelinePost{ID: "123", ConversationID: "ancestor"}}}}
	next, _ := m.openSelectedQuote()
	m = next.(Model)
	if m.threadRootID != "123" {
		t.Fatal("opened quote ancestor")
	}
	next, _ = m.leaveThread()
	m = next.(Model)
	if m.mode != modeNotifications {
		t.Fatal("did not return to notifications")
	}
}

func TestReplyAndQuoteDraftsStaySeparate(t *testing.T) {
	m := NewWithImageMode("off")
	m.width = 80
	m.height = 24
	post := api.TimelinePost{ID: "123", Handle: "alice", Text: "original"}
	next, _ := m.beginReply(post)
	m = next.(Model)
	m.replyEditor.SetValue("reply draft")
	m.saveReplyDraft()
	m.mode = modeFeed
	next, _ = m.beginPostComposer(post, true)
	m = next.(Model)
	if m.replyEditor.Value() != "" {
		t.Fatal("quote inherited reply draft")
	}
	m.replyEditor.SetValue("quote draft")
	m.replyAttachments = []media.Attachment{{ID: "photo"}}
	m.saveReplyDraft()
	m.mode = modeFeed
	next, _ = m.beginReply(post)
	m = next.(Model)
	if m.replyEditor.Value() != "reply draft" || len(m.replyAttachments) != 0 {
		t.Fatal("reply draft overwritten")
	}
	m.mode = modeFeed
	next, _ = m.beginPostComposer(post, true)
	m = next.(Model)
	if m.replyEditor.Value() != "quote draft" || len(m.replyAttachments) != 1 {
		t.Fatal("quote draft lost")
	}
	next, _ = m.updateReply(replyResultMsg{err: errors.New("offline")})
	m = next.(Model)
	if m.mode != modeReply || m.replyEditor.Value() != "quote draft" {
		t.Fatal("failed quote lost draft")
	}
	next, _ = m.updateReply(replyResultMsg{id: "456"})
	m = next.(Model)
	if m.toast != "quote posted" || m.replyDrafts["123"].text != "reply draft" {
		t.Fatal("quote success affected reply")
	}
	if _, ok := m.replyDrafts["quote:123"]; ok {
		t.Fatal("sent quote draft retained")
	}
}

func TestQuoteComposerKeysAndNarrowLayout(t *testing.T) {
	for _, width := range []int{40, 60, 80} {
		m := NewWithImageMode("off")
		m.width = width
		m.height = 30
		m.posts = []api.TimelinePost{{ID: "123", Handle: "alice", Text: "the original post"}}
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}, Alt: true})
		m = next.(Model)
		if m.mode != modeReply || !m.replyQuote {
			t.Fatal("alt+q did not open quote composer")
		}
		content := m.View()
		if !strings.Contains(content, "quote >>@alice") || !strings.Contains(content, "enter quote") || lipgloss.Width(content) > width {
			t.Fatalf("quote layout at %d: %s", width, content)
		}
	}
}
