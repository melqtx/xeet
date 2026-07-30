package timeline

import (
	"context"
	"time"

	"github.com/melqtx/xeet/pkg/api"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

type listsMsg struct {
	lists     []api.ListInfo
	accountID string
	picker    bool
	err       error
}

func fetchListsCmd(parent context.Context, accountID string, picker bool) tea.Cmd {
	return func() tea.Msg {
		mgr, err := openRequestConfigManager()
		if err != nil {
			return listsMsg{accountID: accountID, picker: picker, err: err}
		}
		cfg, err := loadRequestConfig(mgr, accountID)
		if err != nil {
			return listsMsg{accountID: accountID, picker: picker, err: err}
		}
		ctx, cancel := context.WithTimeout(parent, 40*time.Second)
		defer cancel()
		client := api.NewWebClient(cfg)
		lists, err := client.FetchOwnedLists(ctx)
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.SaveQueryIDs(cfg)
		}
		return listsMsg{lists: lists, accountID: accountID, picker: picker, err: err}
	}
}

func (m *Model) beginListPicker() tea.Cmd {
	m.listReturn = m.mode
	m.listPicker = nil
	m.listPickerSel = 0
	m.listPickerErr = nil
	m.listPickerLoading = true
	m.mode = modeListPicker
	// In the add-column flow the account is chosen afterwards, so the lists
	// come from the active account.
	accountID := m.cur().accountID
	if m.listFor == targetNewColumn {
		accountID = ""
	}
	return m.imageRepaint(tea.Batch(
		m.spinner.Tick, fetchListsCmd(m.requestContext(), accountID, true),
	))
}

func (m Model) cancelListPicker() (tea.Model, tea.Cmd) {
	if m.listFor == targetNewColumn {
		m.listFor = targetFocusedColumn
		m.columnDraft = ColumnSpec{}
		m.mode = modeFeed
	} else {
		m.mode = m.listReturn
	}
	m.listPickerLoading = false
	m.syncViewport()
	m.ensureSelectedVisible()
	return m, m.imageRepaint(m.requestPreviews())
}

func (m Model) updateListPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case listsMsg:
		if !msg.picker {
			if msg.err == nil {
				m.nameListColumns(msg.accountID, msg.lists)
			}
			return m, nil
		}
		expected := m.cur().accountID
		if m.listFor == targetNewColumn {
			expected = ""
		}
		if msg.accountID != expected {
			return m, nil
		}
		m.listPickerLoading = false
		m.listPickerErr = msg.err
		if msg.err == nil {
			m.listPicker = msg.lists
			m.listPickerSel = min(m.listPickerSel, max(0, len(m.listPicker)-1))
		}
		return m, nil
	case threadMsg:
		if m.listReturn != modeThread {
			return m, nil
		}
		return m.applyThreadPage(msg, false)
	case pageMsg:
		return m.applyFeedPage(msg)
	case likeMsg:
		m.settleLike(msg)
		return m, nil
	case retweetMsg:
		m.settleRepost(msg)
		return m, nil
	case profileMsg:
		return m, m.applyProfileResult(msg)
	case previewMsg:
		m.storePreview(msg)
		return m, nil
	case spinner.TickMsg:
		if m.listPickerLoading || m.cur().loading || m.cur().loadingMore || m.cur().refreshing || m.cur().threadLoading || m.cur().threadMore {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc":
		return m.cancelListPicker()
	case "j", "down":
		m.listPickerSel = min(max(0, len(m.listPicker)-1), m.listPickerSel+1)
	case "k", "up":
		m.listPickerSel = max(0, m.listPickerSel-1)
	case "enter":
		if m.listPickerLoading || m.listPickerErr != nil ||
			m.listPickerSel < 0 || m.listPickerSel >= len(m.listPicker) {
			return m, nil
		}
		selected := m.listPicker[m.listPickerSel]
		if m.listFor == targetNewColumn {
			m.columnDraft = ColumnSpec{Kind: FeedList, ListID: selected.ID}
			m.listFor = targetFocusedColumn
			m.listPickerLoading = false
			return m, m.beginAccountPicker("new column · account", intentColumnAccount)
		}
		m.cur().listID = selected.ID
		m.cur().listName = selected.Name
		m.listPickerLoading = false
		m.mode = modeFeed
		return m, m.setFeed(FeedList)
	}
	return m, nil
}
