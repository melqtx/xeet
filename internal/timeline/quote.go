package timeline

import tea "github.com/charmbracelet/bubbletea"

type quoteBack struct {
	rootID           string
	mode             mode
	selected, offset int
	expanded         bool
	thread           *threadState
}

func (m Model) openSelectedQuote() (tea.Model, tea.Cmd) {
	post, ok := m.currentPost()
	if !ok || post.Quote == nil || post.Quote.ID == "" {
		return m, m.showToast("no quoted post to open")
	}
	back := quoteBack{rootID: post.Quote.ID, mode: m.mode, selected: m.selected, offset: m.viewport.YOffset, expanded: m.expanded, thread: m.snapshotThread()}
	quote := *post.Quote
	quote.ConversationID = ""
	next, cmd := m.beginThread(quote, m.mode)
	m = next.(Model)
	// A quote is its own focal post, even when opened from notifications.
	m.threadRootID = post.Quote.ID
	m.quoteStack = append(m.quoteStack, back)
	return m, cmd
}

func (m Model) leaveQuote() (tea.Model, tea.Cmd) {
	back := m.quoteStack[len(m.quoteStack)-1]
	m.quoteStack = m.quoteStack[:len(m.quoteStack)-1]
	m.mode, m.selected, m.expanded = back.mode, back.selected, back.expanded
	m.restoreThread(back.thread)
	if m.mode == modeFeed {
		m.selected = m.feedSelected
	}
	m.syncViewport()
	m.viewport.SetYOffset(back.offset)
	return m, m.imageRepaint(m.requestPreviews())
}
