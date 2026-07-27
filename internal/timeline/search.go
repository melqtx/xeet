package timeline

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) beginSearch() tea.Cmd {
	m.searchReturn = m.mode
	m.searchInput.SetValue(m.searchQuery)
	m.mode = modeSearch
	return m.imageRepaint(m.searchInput.Focus())
}

func (m Model) cancelSearch() (tea.Model, tea.Cmd) {
	m.searchInput.Blur()
	// `xeet search` starts directly in an empty prompt. There is no previous
	// feed to reveal in that case, so escape should leave instead of exposing
	// an empty search-results screen.
	if m.searchReturn == modeFeed && m.feed == FeedSearch && m.searchQuery == "" && len(m.posts) == 0 {
		return m, tea.Quit
	}
	m.mode = m.searchReturn
	m.syncViewport()
	m.ensureSelectedVisible()
	return m, m.imageRepaint(m.requestPreviews())
}

func (m Model) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case threadMsg:
		if m.searchReturn != modeThread {
			return m, nil
		}
		return m.applyThreadPage(msg, false)
	case pageMsg:
		return m.applyFeedPage(msg)
	case likeMsg:
		m.settleLike(msg)
		return m, nil
	case previewMsg:
		m.storePreview(msg)
		return m, nil
	case spinner.TickMsg:
		if m.loading || m.loadingMore || m.refreshing || m.threadLoading || m.threadMore {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return m.cancelSearch()
		case "enter":
			query := strings.TrimSpace(m.searchInput.Value())
			if query == "" {
				return m.cancelSearch()
			}
			m.searchQuery = query
			m.searchInput.Blur()
			m.mode = modeFeed
			return m, m.setFeed(FeedSearch)
		}
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}
