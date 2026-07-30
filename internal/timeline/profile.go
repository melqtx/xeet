package timeline

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/melqtx/xeet/pkg/api"
)

// profileMsg carries the handle→id resolution for a FeedProfile column.
// Posts only carry the author's handle while UserTweets pages by numeric id,
// so opening a profile is two hops: resolve once, then page by id.
type profileMsg struct {
	colID  int
	handle string
	userID string
	err    error
}

func resolveProfile(parent context.Context, colID int, accountID, handle string) tea.Cmd {
	return func() tea.Msg {
		mgr, err := openRequestConfigManager()
		if err != nil {
			return profileMsg{colID: colID, handle: handle, err: err}
		}
		cfg, err := loadRequestConfig(mgr, accountID)
		if err != nil {
			return profileMsg{colID: colID, handle: handle, err: err}
		}
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		defer cancel()
		client := api.NewWebClient(cfg)
		userID, err := client.FetchUserByScreenName(ctx, handle)
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.SaveQueryIDs(cfg)
		}
		return profileMsg{colID: colID, handle: handle, userID: userID, err: err}
	}
}

// beginProfile switches the focused column to the selected post author's
// timeline, in place — the same way f, b, L, and / replace the feed rather
// than opening another column.
func (m Model) beginProfile(post api.TimelinePost) (tea.Model, tea.Cmd) {
	c := m.cur()
	c.profileHandle = post.Handle
	c.profileUserID = ""
	// resetColumnFeed's fetch cmd is dropped on purpose: UserTweets cannot run
	// until the handle resolves, so resolveProfile kicks the first page.
	_ = m.resetColumnFeed(c, FeedProfile)
	m.mode = modeFeed
	m.syncViewport()
	return m, m.imageRepaint(tea.Batch(m.spinner.Tick, resolveProfile(
		m.requestContext(), c.id, c.accountID, post.Handle,
	)))
}

func (m *Model) applyProfileResult(msg profileMsg) tea.Cmd {
	c := m.columnByID(msg.colID)
	if c == nil || c.feed != FeedProfile {
		return nil
	}
	if msg.err != nil {
		c.loading = false
		c.err = msg.err
		return m.showToast("couldn't load @" + msg.handle)
	}
	c.profileUserID = msg.userID
	return tea.Batch(m.spinner.Tick, fetchPageSeq(
		m.requestContext(), c.feed, c.searchQuery, c.listID, c.profileUserID, c.accountID, "", false, c.feedSeq, c.id, false,
	))
}
