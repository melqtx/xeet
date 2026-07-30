package timeline

import (
	"github.com/melqtx/xeet/pkg/api"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// maxColumns matches the --columns flag's limit; more panes than that leave
// each one too narrow to read.
const maxColumns = 4

// column is one independently scrolling feed pane. With one column the TUI
// behaves exactly as before; the struct exists so state cannot leak between
// panes once there are several.
type column struct {
	id          int // stable identity for message routing; never reused
	feed        FeedKind
	accountID   string
	searchQuery string
	listID      string
	listName    string
	// profileHandle is shown in the header; profileUserID is the resolved
	// id UserTweets pages by. Kept separate so pagination never re-resolves.
	profileHandle string
	profileUserID string
	feedSeq       int
	posts         []api.TimelinePost
	cursor        string
	selected      int
	starts        []int
	ends          []int
	loading       bool
	loadingMore   bool
	refreshing    bool
	expanded      bool
	err           error
	viewport      viewport.Model
	wezFrameKey   string

	// thread cluster — the thread overlay is full-screen (decision #2) but its
	// STATE belongs to the column that opened it, so returning from the overlay
	// restores that column exactly.
	feedSelected  int
	threadRootID  string
	threadPosts   []api.ConversationPost
	threadCursor  string
	threadLoading bool
	threadMore    bool
	threadErr     error
	threadSeq     int
}

func (m *Model) cur() *column { return &m.columns[m.focus] }

func (m *Model) columnByID(id int) *column {
	for i := range m.columns {
		if m.columns[i].id == id {
			return &m.columns[i]
		}
	}
	return nil
}

func newColumn(id int) column {
	return column{
		id:       id,
		loading:  true,
		viewport: viewport.New(72, 18),
	}
}

func (m *Model) configureColumns(specs []ColumnSpec) {
	if len(specs) == 0 {
		specs = []ColumnSpec{{Kind: FeedForYou}}
	}
	firstID := m.nextColID
	if len(m.columns) > 0 {
		firstID = m.columns[0].id
	} else {
		m.nextColID++
	}
	m.columns = []column{newColumn(firstID)}
	for len(m.columns) < len(specs) {
		m.columns = append(m.columns, newColumn(m.nextColID))
		m.nextColID++
	}
	for index, spec := range specs {
		c := &m.columns[index]
		c.feed = spec.Kind
		c.accountID = spec.AccountID
		c.searchQuery = spec.Query
		c.listID = spec.ListID
		c.profileHandle = spec.ProfileHandle
		if spec.Kind == FeedList {
			// A spec carries only the id, so the header shows that until the
			// lists request lands and names it.
			c.listName = spec.ListID
		}
	}
	m.focus = 0
	m.enforceMultiColumnImageMode()
}

// addColumn appends a pane and focuses it. A page arriving for an id that no
// longer exists is dropped by columnByID, so ids are never reused.
func (m *Model) addColumn(spec ColumnSpec) tea.Cmd {
	if len(m.columns) >= maxColumns {
		return m.showToast("already at 4 columns")
	}
	c := newColumn(m.nextColID)
	m.nextColID++
	c.feed = spec.Kind
	c.accountID = spec.AccountID
	c.searchQuery = spec.Query
	c.listID = spec.ListID
	c.profileHandle = spec.ProfileHandle
	if spec.Kind == FeedList {
		c.listName = spec.ListID
	}
	m.columns = append(m.columns, c)
	m.focus = len(m.columns) - 1
	m.enforceMultiColumnImageMode()
	m.resize()
	cmds := []tea.Cmd{m.spinner.Tick, fetchPageSeq(
		m.requestContext(), c.feed, c.searchQuery, c.listID, c.profileUserID, c.accountID, "", false, c.feedSeq, c.id, false,
	)}
	if c.feed == FeedList {
		cmds = append(cmds, fetchListsCmd(m.requestContext(), c.accountID, false))
	}
	return m.imageRepaint(tea.Batch(cmds...))
}

// removeColumn drops the focused pane. In-flight pages for it die against the
// nil from columnByID; the preview cache only needs a budget re-check.
func (m *Model) removeColumn() tea.Cmd {
	if len(m.columns) <= 1 {
		return m.showToast("keep at least one column")
	}
	m.columns = append(m.columns[:m.focus], m.columns[m.focus+1:]...)
	m.focus = min(m.focus, len(m.columns)-1)
	m.evictDistantPreviews()
	m.resize()
	return m.imageRepaint(m.requestPreviews())
}

// setColumnAccount repoints the focused pane at another saved account while
// keeping its feed kind, query, and list. The seq bump in resetColumnFeed
// retires any page the old account still has in flight.
func (m *Model) setColumnAccount(accountID string) tea.Cmd {
	c := m.cur()
	if c.accountID == accountID {
		return m.showToast("already " + m.accountLabelFor(accountID))
	}
	c.accountID = accountID
	if c.feed == FeedList {
		// The name came from the old account's lists; show the id until the
		// new account's enumeration confirms what it can actually see.
		c.listName = c.listID
	}
	refetch := m.resetColumnFeed(c, c.feed)
	cmds := []tea.Cmd{m.spinner.Tick, refetch}
	if c.feed == FeedList {
		cmds = append(cmds, fetchListsCmd(m.requestContext(), accountID, false))
	}
	m.syncViewport()
	return m.imageRepaint(tea.Batch(cmds...))
}

func (m Model) accountLabelFor(accountID string) string {
	if accountID == "" {
		return "the active account"
	}
	for _, account := range m.accounts {
		if account.UserID == accountID {
			return accountInfoLabel(account)
		}
	}
	return accountID
}

// namesPendingForListColumns reports whether any column is still showing a raw
// list id where its name belongs.
func (m *Model) namesPendingForListColumns() bool {
	for i := range m.columns {
		if c := &m.columns[i]; c.feed == FeedList && c.listName == c.listID {
			return true
		}
	}
	return false
}

func (m *Model) nameListColumns(accountID string, lists []api.ListInfo) {
	byID := make(map[string]string, len(lists))
	for _, l := range lists {
		byID[l.ID] = l.Name
	}
	for i := range m.columns {
		c := &m.columns[i]
		if c.feed != FeedList || c.accountID != accountID {
			continue
		}
		// Only fill the placeholder: a name the picker already resolved is
		// better than one from a list the account has since stopped following.
		if c.listName == c.listID {
			if name := byID[c.listID]; name != "" {
				c.listName = name
			}
		}
	}
}
