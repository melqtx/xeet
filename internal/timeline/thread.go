package timeline

// Thread mode: the conversation opened with enter from the feed. It reuses the
// feed's rendering and post actions; what lives here is the reply tree itself
// -- fetching a TweetDetail page, resolving each reply's depth, and moving
// through the result.

import (
	"context"
	"errors"
	"time"

	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func fetchThread(parent context.Context, tweetID, cursor string, more bool, seq int) tea.Cmd {
	return func() tea.Msg {
		mgr, err := config.NewConfigManager()
		if err != nil {
			return threadMsg{rootID: tweetID, seq: seq, err: err, more: more}
		}
		cfg, err := mgr.Load()
		if err != nil {
			return threadMsg{rootID: tweetID, seq: seq, err: err, more: more}
		}
		ctx, cancel := context.WithTimeout(parent, 40*time.Second)
		defer cancel()
		client := api.NewWebClient(cfg)
		page, err := client.FetchTweetDetail(ctx, tweetID, cursor, 40)
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.Save(cfg)
		}
		return threadMsg{rootID: tweetID, seq: seq, page: page, err: err, more: more}
	}
}

func (m *Model) requestThread(cursor string, more bool) tea.Cmd {
	m.threadSeq++
	return fetchThread(m.requestContext(), m.threadRootID, cursor, more, m.threadSeq)
}

func (m Model) applyThreadPage(msg threadMsg, repaint bool) (tea.Model, tea.Cmd) {
	if msg.rootID != m.threadRootID || msg.seq != m.threadSeq {
		return m, nil
	}
	m.threadLoading = false
	m.threadMore = false
	if msg.err != nil {
		m.threadErr = msg.err
		return m, nil
	}
	m.threadErr = nil
	selectedID := ""
	if m.selected >= 0 && m.selected < len(m.threadPosts) {
		selectedID = m.threadPosts[m.selected].ID
	}
	seen := map[string]bool{}
	var merged []api.ConversationPost
	if msg.more {
		merged = append(merged, m.threadPosts...)
		for _, post := range merged {
			seen[post.ID] = true
		}
	}
	for _, post := range m.resolveConversationPosts(msg.page) {
		if !seen[post.ID] {
			merged = append(merged, post)
			seen[post.ID] = true
		}
	}
	// X can omit the focal post from a refresh while still returning replies.
	// The conversation must always keep its root: without it a refresh would
	// silently turn the thread into a list of orphaned replies.
	if !seen[m.threadRootID] {
		if root, ok := m.threadRootPost(); ok {
			merged = append([]api.ConversationPost{root}, merged...)
		}
	}
	m.threadPosts = merged
	m.normalizeThreadDepths()
	m.threadCursor = msg.page.Cursor
	m.selected = indexOfConversationPost(m.threadPosts, selectedID)
	if m.selected < 0 {
		m.selected = 0
	}
	if repaint {
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, m.imageRepaint(m.requestPreviews())
	}
	return m, nil
}

func (m Model) threadRootPost() (api.ConversationPost, bool) {
	for _, post := range m.threadPosts {
		if post.ID == m.threadRootID {
			post.Depth = 0
			return post, true
		}
	}
	return api.ConversationPost{}, false
}

func (m Model) resolveConversationPosts(page *api.ConversationPage) []api.ConversationPost {
	resolved := append([]api.ConversationPost(nil), page.Posts...)
	depths := make(map[string]int, len(m.threadPosts)+len(resolved))
	for _, post := range m.threadPosts {
		depths[post.ID] = post.Depth
	}
	for _, post := range resolved {
		depths[post.ID] = post.Depth
	}
	pending := append([]api.TimelinePost(nil), page.Unresolved...)
	for len(pending) > 0 {
		progress := false
		next := pending[:0]
		for _, post := range pending {
			parentDepth, ok := depths[post.InReplyToID]
			if !ok {
				next = append(next, post)
				continue
			}
			depth := parentDepth + 1
			resolved = append(resolved, api.ConversationPost{TimelinePost: post, Depth: depth})
			depths[post.ID] = depth
			progress = true
		}
		if !progress {
			break
		}
		pending = next
	}
	return resolved
}

func (m *Model) normalizeThreadDepths() {
	byID := make(map[string]api.ConversationPost, len(m.threadPosts))
	for _, post := range m.threadPosts {
		byID[post.ID] = post
	}
	for i := range m.threadPosts {
		if m.threadPosts[i].ID == m.threadRootID {
			m.threadPosts[i].Depth = 0
			continue
		}
		depth := 1
		parent := m.threadPosts[i].InReplyToID
		visited := map[string]bool{m.threadPosts[i].ID: true}
		for parent != "" && parent != m.threadRootID && !visited[parent] {
			visited[parent] = true
			ancestor, ok := byID[parent]
			if !ok {
				break
			}
			parent = ancestor.InReplyToID
			depth++
		}
		m.threadPosts[i].Depth = depth
	}
}

func (m Model) updateThread(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.threadLoading || m.threadMore || m.zoomLoading() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case pageMsg:
		return m.applyFeedPage(msg)
	case threadMsg:
		return m.applyThreadPage(msg, true)
	case likeMsg:
		return m, m.applyLikeResult(msg)
	case previewMsg:
		return m, m.applyPreview(msg)
	case actionMsg:
		if msg.err != nil {
			return m, m.showToast(msg.err.Error())
		}
		return m, m.showToast(msg.message)
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeFeed
		m.selected = m.feedSelected
		m.threadSeq++
		m.threadRootID = ""
		m.expanded = false
		m.threadLoading = false
		m.threadMore = false
		// An expired session affects every request, not just this thread.
		// Hand the error to the feed so its reconnect flow stays reachable.
		if errors.Is(m.threadErr, api.ErrSessionExpired) {
			m.err = m.threadErr
		}
		m.threadErr = nil
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, m.imageRepaint()
	case "a":
		if errors.Is(m.threadErr, api.ErrSessionExpired) {
			m.action = Action{Kind: ActionAuthenticate}
			return m, tea.Quit
		}
	case "ctrl+l":
		m.syncViewport()
		return m, func() tea.Msg { return tea.ClearScreen() }
	case "?", "f1":
		m.help = true
		return m, m.imageRepaint()
	case "j", "down":
		m.moveThreadSelection(m.selected + 1)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMoreThread())
	case "k", "up":
		m.moveThreadSelection(m.selected - 1)
		return m, m.imageRepaint(m.requestPreviews())
	case "ctrl+d":
		m.moveThreadSelection(m.selected + 5)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMoreThread())
	case "ctrl+u":
		m.moveThreadSelection(m.selected - 5)
		return m, m.imageRepaint(m.requestPreviews())
	case "g", "home":
		m.moveThreadSelection(0)
		return m, m.imageRepaint(m.requestPreviews())
	case "G", "end":
		m.moveThreadSelection(len(m.threadPosts) - 1)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMoreThread())
	case "R", "ctrl+r":
		m.threadLoading = true
		m.threadMore = false
		m.threadErr = nil
		return m, m.imageRepaint(tea.Batch(m.spinner.Tick, m.requestThread("", false)))
	case "r":
		if post, ok := m.currentPost(); ok {
			return m.beginReply(post)
		}
	case "/":
		return m, m.beginSearch()
	case "enter", " ", "e":
		m.expanded = !m.expanded
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, m.imageRepaint()
	case "o":
		return m, m.openSelected()
	case "A":
		return m, m.showAltText()
	case "i":
		return m, m.zoomSelected()
	case "l":
		return m, m.toggleSelectedLike()
	case "y":
		return m, m.copySelectedLink()
	}
	return m, nil
}

func (m *Model) moveThreadSelection(target int) {
	if len(m.threadPosts) == 0 {
		return
	}
	m.selected = max(0, min(len(m.threadPosts)-1, target))
	m.expanded = false
	m.toast = ""
	m.syncViewport()
	m.ensureSelectedVisible()
}

func (m *Model) maybeLoadMoreThread() tea.Cmd {
	if len(m.threadPosts) > 0 && m.selected >= len(m.threadPosts)-5 && m.threadCursor != "" && !m.threadLoading && !m.threadMore {
		m.threadMore = true
		return tea.Batch(m.spinner.Tick, m.requestThread(m.threadCursor, true))
	}
	return nil
}

func indexOfConversationPost(posts []api.ConversationPost, id string) int {
	if id == "" {
		return 0
	}
	for i := range posts {
		if posts[i].ID == id {
			return i
		}
	}
	return -1
}
