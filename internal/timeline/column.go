package timeline

import (
	"github.com/melqtx/xeet/pkg/api"

	"github.com/charmbracelet/bubbles/viewport"
)

// column is one independently scrolling feed pane. With one column the TUI
// behaves exactly as before; the struct exists so state cannot leak between
// panes once there are several.
type column struct {
	id          int // stable identity for message routing; never reused
	feed        FeedKind
	searchQuery string
	listID      string
	listName    string
	feedSeq     int
	posts       []api.TimelinePost
	cursor      string
	selected    int
	starts      []int
	ends        []int
	loading     bool
	loadingMore bool
	refreshing  bool
	expanded    bool
	err         error
	viewport    viewport.Model
	wezFrameKey string

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
		c.searchQuery = spec.Query
		c.listID = spec.ListID
		if spec.Kind == FeedList {
			// A spec carries only the id, so the header shows that until the
			// lists request lands and names it.
			c.listName = spec.ListID
		}
	}
	m.focus = 0
	m.enforceMultiColumnImageMode()
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

func (m *Model) nameListColumns(lists []api.ListInfo) {
	byID := make(map[string]string, len(lists))
	for _, l := range lists {
		byID[l.ID] = l.Name
	}
	for i := range m.columns {
		c := &m.columns[i]
		if c.feed != FeedList {
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
