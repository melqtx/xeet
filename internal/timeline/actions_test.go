package timeline

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/melqtx/xeet/internal/media"
	"github.com/melqtx/xeet/pkg/api"
)

func TestRepostTogglePreventsDuplicateAndRollsBackCachedFeed(t *testing.T) {
	m := NewWithImageMode("off")
	m.loading = false
	m.posts = posts(1)
	if cmd := m.toggleSelectedRepost(); cmd == nil {
		t.Fatal("no repost request")
	}
	if !m.posts[0].Reposted || m.posts[0].RepostCount != 1 || m.toggleSelectedRepost() != nil {
		t.Fatal("optimistic state or duplicate protection failed")
	}
	m.setFeed(FeedFollowing)
	m = update(t, m, repostMsg{id: "a", reposted: true, err: errors.New("rejected")})
	m.setFeed(FeedForYou)
	if m.posts[0].Reposted || m.posts[0].RepostCount != 0 || m.reposting["a"] {
		t.Fatal("failed repost survived in cached feed")
	}
	m.applyRepost("a", true)
	if cmd := m.toggleSelectedRepost(); cmd == nil || m.posts[0].Reposted {
		t.Fatal("undo did not start")
	}
}

func TestReplyImagesPreservedOnFailureAndWhenReopened(t *testing.T) {
	m := NewWithImageMode("off")
	next, _ := m.beginReply(api.TimelinePost{ID: "parent"})
	m = next.(Model)
	a := media.Attachment{ID: "image", Name: "cat.png", MIME: "image/png", Data: []byte("data"), Width: 10, Height: 10}
	if err := m.addReplyAttachment(a); err != nil {
		t.Fatal(err)
	}
	if err := m.addReplyAttachment(a); err == nil {
		t.Fatal("duplicate attachment accepted")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.replyPosting {
		t.Fatal("image-only reply cannot send")
	}
	m = update(t, m, replyResultMsg{err: errors.New("upload failed")})
	if len(m.replyAttachments) != 1 || m.replyErr == nil {
		t.Fatal("failed upload discarded attachment")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	next, _ = m.beginReply(api.TimelinePost{ID: "parent"})
	m = next.(Model)
	if len(m.replyAttachments) != 1 {
		t.Fatal("reopening reply discarded draft")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if len(m.replyAttachments) != 0 || m.replyAttachmentsFocused {
		t.Fatal("attachment removal failed")
	}
}

func TestReplyMediaLoadingDoesNotRaceSendOrNextReply(t *testing.T) {
	m := NewWithImageMode("off")
	next, _ := m.beginReply(api.TimelinePost{ID: "first"})
	m = next.(Model)
	m.replyEditor.SetValue("hello")
	m.loadReplyMedia("unused", false)
	seq := m.replyMediaSeq
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.replyPosting {
		t.Fatal("sent while image was loading")
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	next, _ = m.beginReply(api.TimelinePost{ID: "second"})
	m = next.(Model)
	m = update(t, m, replyMediaMsg{seq: seq, attachment: &media.Attachment{ID: "late"}})
	if len(m.replyAttachments) != 0 {
		t.Fatal("late attachment leaked into another reply")
	}
}

func TestReplyAttachmentViewFitsAndShowsActions(t *testing.T) {
	for _, width := range []int{34, 42, 80} {
		m := NewWithImageMode("off")
		m.width, m.height = width, 24
		next, _ := m.beginReply(api.TimelinePost{ID: "parent", Handle: "someone", Text: "original"})
		m = next.(Model)
		for i := 0; i < 4; i++ {
			if err := m.addReplyAttachment(media.Attachment{ID: string(rune('a' + i)), Name: "image.png", Width: 100, Height: 100}); err != nil {
				t.Fatal(err)
			}
		}
		view := m.View()
		if maxLineWidth(view) > width || lipgloss.Height(view) > 24 {
			t.Fatalf("reply overflow at %d: %dx%d\n%s", width, maxLineWidth(view), lipgloss.Height(view), view)
		}
		if !strings.Contains(view, "ctrl+o") || !strings.Contains(view, "image.png") {
			t.Fatal("attachment controls missing")
		}
	}
}
