package timeline

// Reply mode: the small composer that opens over the feed or a thread with r.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/melqtx/xeet/internal/media"
	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func sendReplyOrQuote(parent context.Context, tweetID, text string, quote bool, attachments ...media.Attachment) tea.Cmd {
	attachments = append([]media.Attachment(nil), attachments...)
	return func() tea.Msg {
		mgr, err := config.NewConfigManager()
		if err != nil {
			return replyResultMsg{err: err}
		}
		cfg, err := mgr.Load()
		if err != nil {
			return replyResultMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
		defer cancel()
		client := api.NewWebClient(cfg)
		uploads := make([]api.Upload, 0, len(attachments))
		for _, a := range attachments {
			uploads = append(uploads, api.Upload{Filename: a.Name, ContentType: a.MIME, Data: a.Data})
		}
		var id string
		if quote {
			id, err = client.PostQuote(ctx, text, tweetID, uploads, nil)
		} else {
			id, err = client.PostTweet(ctx, text, tweetID, uploads, nil)
		}
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.Save(cfg)
		}
		return replyResultMsg{id: id, err: err}
	}
}

func (m Model) beginReply(post api.TimelinePost) (tea.Model, tea.Cmd) {
	return m.beginPostComposer(post, false)
}

func (m Model) beginPostComposer(post api.TimelinePost, quote bool) (tea.Model, tea.Cmd) {
	m.replyQuote = quote
	m.replyOffset, m.replyExpanded = m.viewport.YOffset, m.expanded
	m.replyReturn = m.mode
	m.mode = modeReply
	m.replyPost = post
	m.replyErr = nil
	m.replyNotice = ""
	m.replyEditor.Reset()
	m.replyEditor.Placeholder = "write your reply…"
	if quote {
		m.replyEditor.Placeholder = "add your take…"
	}
	m.replyMediaSeq++
	m.replyMediaLoading = false
	m.replyAttachmentsFocused = false
	m.replyPathOpen = false
	m.replyPath = textinput.New()
	m.replyPath.Prompt = "> "
	m.replyPath.Placeholder = "~/Pictures/photo.png"
	m.replyPath.CharLimit = 4096
	draft := m.replyDrafts[m.replyDraftKey()]
	m.replyEditor.SetValue(draft.text)
	m.replyAttachments = append([]media.Attachment(nil), draft.attachments...)
	m.replyAttachmentSelected = 0
	m.resize()
	return m, m.imageRepaint(m.replyEditor.Focus())
}

func (m Model) updateReply(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case replyMediaMsg:
		if msg.seq != m.replyMediaSeq {
			return m, nil
		}
		m.replyMediaLoading = false
		m.replyErr = msg.err
		if msg.err == nil && msg.attachment != nil {
			m.replyErr = m.addReplyAttachment(*msg.attachment)
		}
		if msg.err == nil && msg.text != "" {
			m.replyEditor.InsertString(msg.text)
		}
		return m, nil
	case threadMsg:
		if m.replyReturn == modeThread {
			return m.applyThreadPage(msg, false)
		}
		if m.replyReturn == modeNotifications && m.notificationReturn == modeThread {
			return m.applyThreadPageBehindNotifications(msg)
		}
		return m, nil
	case pageMsg:
		return m.applyFeedPage(msg)
	case likeMsg:
		// The composer covers the feed, so a like settles silently here: no
		// toast, and no re-render of a list nobody can see.
		m.settleLike(msg)
		return m, nil
	case previewMsg:
		m.storePreview(msg)
		return m, nil
	case spinner.TickMsg:
		if m.replyPosting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case replyResultMsg:
		if m.replyCancel != nil {
			m.replyCancel()
			m.replyCancel = nil
		}
		m.replyPosting = false
		if msg.err != nil {
			m.replyErr = msg.err
			m.replyNotice = ""
			return m, m.replyEditor.Focus()
		}
		delete(m.replyDrafts, m.replyDraftKey())
		m.replyAttachments = nil
		m.mode = m.replyReturn
		m.replyEditor.Blur()
		m.replyEditor.Reset()
		label := "reply sent ♥"
		if m.replyQuote {
			label = "quote posted"
		}
		toast := m.showToast(label)
		m.syncViewport()
		m.ensureSelectedVisible()
		if m.mode == modeThread && !m.replyQuote {
			m.threadLoading = true
			m.threadMore = false
			return m, m.imageRepaint(tea.Batch(toast, m.spinner.Tick, m.requestThread("", false)))
		}
		return m, m.imageRepaint(toast)
	case replyBrowserMsg:
		if msg.err != nil {
			m.replyErr = fmt.Errorf("couldn't open X: %w", msg.err)
			m.replyNotice = ""
			return m, nil
		}
		m.replyErr = nil
		m.replyNotice = "opened reply in X"
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if ok {
		if m.replyPosting {
			if (key.String() == "esc" || key.String() == "ctrl+c") && m.replyCancel != nil {
				m.replyCancel()
				m.replyNotice = "cancelling…"
			}
			return m, nil
		}
		if m.replyPathOpen {
			switch key.String() {
			case "esc":
				m.replyPathOpen = false
				return m, m.replyEditor.Focus()
			case "enter":
				m.replyPathOpen = false
				m.replyErr = nil
				return m, tea.Batch(m.replyEditor.Focus(), m.loadReplyMedia(m.replyPath.Value(), false))
			}
			var cmd tea.Cmd
			m.replyPath, cmd = m.replyPath.Update(msg)
			return m, cmd
		}
		if m.replyAttachmentsFocused {
			switch key.String() {
			case "tab", "enter", "esc":
				m.replyAttachmentsFocused = false
				return m, m.replyEditor.Focus()
			case "left", "up", "shift+tab":
				m.replyAttachmentSelected = max(0, m.replyAttachmentSelected-1)
			case "right", "down":
				m.replyAttachmentSelected = min(len(m.replyAttachments)-1, m.replyAttachmentSelected+1)
			case "backspace", "delete", "ctrl+x":
				i := m.replyAttachmentSelected
				if i >= 0 && i < len(m.replyAttachments) {
					m.replyAttachments = append(m.replyAttachments[:i], m.replyAttachments[i+1:]...)
					m.replyAttachmentSelected = max(0, min(i, len(m.replyAttachments)-1))
					m.resize()
				}
				if len(m.replyAttachments) == 0 {
					m.replyAttachmentsFocused = false
					return m, m.replyEditor.Focus()
				}
			}
			return m, nil
		}
		switch key.String() {
		case "ctrl+o":
			if m.replyMediaLoading {
				return m, nil
			}
			m.replyPathOpen = true
			m.replyPath.SetValue("")
			m.replyEditor.Blur()
			return m, m.replyPath.Focus()
		case "ctrl+v":
			if m.replyMediaLoading {
				return m, nil
			}
			return m, m.loadReplyMedia("", true)
		case "tab":
			if len(m.replyAttachments) > 0 {
				m.replyAttachmentsFocused = true
				m.replyEditor.Blur()
			}
			return m, nil
		case "b":
			if !m.replyQuote && canOpenReplyInX(m.replyErr) && len(m.replyAttachments) == 0 {
				return m, openReplyInX(m.replyPost.ID, m.replyEditor.Value())
			}
		case "esc", "ctrl+c":
			m.saveReplyDraft()
			m.replyMediaSeq++
			m.mode = m.replyReturn
			m.replyEditor.Blur()
			m.replyErr = nil
			m.replyNotice = ""
			m.expanded = m.replyExpanded
			m.syncViewport()
			m.viewport.SetYOffset(m.replyOffset)
			return m, m.imageRepaint(m.requestPreviews())
		case "enter":
			if m.replyMediaLoading {
				return m, nil
			}
			if strings.TrimSpace(m.replyEditor.Value()) == "" && len(m.replyAttachments) == 0 {
				m.replyErr = fmt.Errorf("write a reply first")
				if m.replyQuote {
					m.replyErr = fmt.Errorf("add a comment or image first")
				}
				return m, nil
			}
			if err := api.ValidatePostText(m.replyEditor.Value()); err != nil {
				m.replyErr = err
				return m, nil
			}
			m.replyPosting = true
			m.replyErr = nil
			m.replyNotice = ""
			m.replyEditor.Blur()
			ctx, cancel := context.WithCancel(m.requestContext())
			m.replyCancel = cancel
			return m, tea.Batch(m.spinner.Tick, sendReplyOrQuote(ctx, m.replyPost.ID, m.replyEditor.Value(), m.replyQuote, m.replyAttachments...))
		case "alt+enter", "ctrl+j":
			m.replyEditor.InsertString("\n")
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.replyEditor, cmd = m.replyEditor.Update(msg)
	return m, cmd
}

func canOpenReplyInX(err error) bool {
	var automated *api.AutomationBlockedError
	if errors.As(err, &automated) {
		return true
	}
	var restricted *api.PostingRestrictedError
	if errors.As(err, &restricted) {
		return true
	}
	var recent *api.RecentlyPostedError
	if errors.As(err, &recent) {
		return true
	}
	var ambiguous *api.AmbiguousPostError
	return errors.As(err, &ambiguous)
}
