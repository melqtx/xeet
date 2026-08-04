package timeline

// Reply mode: the small composer that opens over the feed or a thread with r.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func sendReply(parent context.Context, tweetID, text string) tea.Cmd {
	return func() tea.Msg {
		mgr, err := config.NewConfigManager()
		if err != nil {
			return replyResultMsg{err: err}
		}
		cfg, err := mgr.Load()
		if err != nil {
			return replyResultMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(parent, 40*time.Second)
		defer cancel()
		client := api.NewWebClient(cfg)
		id, err := client.PostTweet(ctx, text, tweetID, nil, nil)
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.Save(cfg)
		}
		return replyResultMsg{id: id, err: err}
	}
}

func (m Model) beginReply(post api.TimelinePost) (tea.Model, tea.Cmd) {
	m.replyReturn = m.mode
	m.mode = modeReply
	m.replyPost = post
	m.replyErr = nil
	m.replyNotice = ""
	m.replyEditor.Reset()
	m.resize()
	return m, m.imageRepaint(m.replyEditor.Focus())
}

func (m Model) updateReply(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		m.replyPosting = false
		if msg.err != nil {
			m.replyErr = msg.err
			m.replyNotice = ""
			return m, m.replyEditor.Focus()
		}
		m.mode = m.replyReturn
		m.replyEditor.Blur()
		m.replyEditor.Reset()
		toast := m.showToast("reply sent ♥")
		m.syncViewport()
		m.ensureSelectedVisible()
		if m.mode == modeThread {
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
			return m, nil
		}
		switch key.String() {
		case "b":
			if canOpenReplyInX(m.replyErr) {
				return m, openReplyInX(m.replyPost.ID, m.replyEditor.Value())
			}
		case "esc", "ctrl+c":
			m.mode = m.replyReturn
			m.replyEditor.Blur()
			m.replyErr = nil
			m.replyNotice = ""
			m.syncViewport()
			return m, m.imageRepaint(m.requestPreviews())
		case "enter":
			if strings.TrimSpace(m.replyEditor.Value()) == "" {
				m.replyErr = fmt.Errorf("write a reply first")
				return m, nil
			}
			m.replyPosting = true
			m.replyErr = nil
			m.replyNotice = ""
			m.replyEditor.Blur()
			return m, tea.Batch(m.spinner.Tick, sendReply(m.requestContext(), m.replyPost.ID, m.replyEditor.Value()))
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
