package timeline

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
	"github.com/charmbracelet/lipgloss"
)

const (
	notificationPollInterval = time.Minute
	notificationMaxBackoff   = 5 * time.Minute
	notificationPopupTime    = 8 * time.Second
	notificationPopupQueue   = 3
	notificationHistoryLimit = 300
)

type notificationMsg struct {
	page                *api.NotificationPage
	err                 error
	more, poll          bool
	seq                 int
	deliveredID, readID string
	accountID           string
}

type notificationPollTickMsg struct{ seq int }
type notificationPopupClearMsg struct{ seq int }
type notificationStateSavedMsg struct {
	err                            error
	accountID, deliveredID, readID string
}

func fetchNotifications(parent context.Context, cursor string, more, poll bool, seq int) tea.Cmd {
	return func() tea.Msg {
		mgr, err := config.NewConfigManager()
		if err != nil {
			return notificationMsg{err: err, more: more, poll: poll, seq: seq}
		}
		cfg, err := mgr.Load()
		if err != nil {
			return notificationMsg{err: err, more: more, poll: poll, seq: seq}
		}
		ctx, cancel := context.WithTimeout(parent, 40*time.Second)
		defer cancel()
		client := api.NewWebClient(cfg)
		count := 30
		if cursor == "" && !more {
			count = 100
		}
		page, err := client.FetchNotifications(ctx, cursor, count)
		if err == nil && cursor == "" && !more && cfg.NotificationsDeliveredID != "" &&
			(cfg.NotificationsAccountID == "" || page.AccountID == "" || page.AccountID == cfg.NotificationsAccountID) {
			page, err = fetchNotificationChanges(ctx, client, page, cfg.NotificationsDeliveredID, count)
		}
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.Save(cfg)
		}
		return notificationMsg{
			page: page, err: err, more: more, poll: poll, seq: seq,
			deliveredID: cfg.NotificationsDeliveredID, readID: cfg.NotificationsReadID,
			accountID: cfg.NotificationsAccountID,
		}
	}
}

func fetchNotificationChanges(ctx context.Context, client *api.WebClient, first *api.NotificationPage, deliveredID string, count int) (*api.NotificationPage, error) {
	if first == nil || deliveredID == "" || notificationPageReached(first, deliveredID) {
		return first, nil
	}
	seenIDs := make(map[string]bool, len(first.Notifications))
	for _, item := range first.Notifications {
		seenIDs[item.ID] = true
	}
	seenCursors := map[string]bool{}
	page := first
	for page.Cursor != "" {
		cursor := page.Cursor
		if seenCursors[cursor] {
			return nil, fmt.Errorf("notifications pagination repeated a cursor")
		}
		seenCursors[cursor] = true
		next, err := client.FetchNotifications(ctx, cursor, count)
		if err != nil {
			return nil, err
		}
		for _, item := range next.Notifications {
			if !seenIDs[item.ID] {
				first.Notifications = append(first.Notifications, item)
				seenIDs[item.ID] = true
			}
		}
		first.Cursor = next.Cursor
		if first.AccountID == "" {
			first.AccountID = next.AccountID
		}
		if notificationPageReached(next, deliveredID) {
			break
		}
		page = next
	}
	return first, nil
}

func notificationPageReached(page *api.NotificationPage, deliveredID string) bool {
	if page == nil {
		return false
	}
	for _, item := range page.Notifications {
		if item.ID == deliveredID || !snowflakeAfter(item.ID, deliveredID) {
			return true
		}
	}
	return false
}

func saveNotificationState(accountID, deliveredID, readID string) tea.Cmd {
	return func() tea.Msg {
		mgr, err := config.NewConfigManager()
		if err == nil {
			err = mgr.SaveNotificationState(accountID, deliveredID, readID)
		}
		return notificationStateSavedMsg{
			err: err, accountID: accountID, deliveredID: deliveredID, readID: readID,
		}
	}
}

func (m *Model) persistNotificationState() tea.Cmd {
	m.notificationStateDirty = true
	return saveNotificationState(m.notificationAccountID, m.notificationDeliveredID, m.notificationReadID)
}

func snowflakeAfter(left, right string) bool {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if left == "" {
		return false
	}
	if right == "" {
		return true
	}
	if len(left) != len(right) {
		return len(left) > len(right)
	}
	return left > right
}

func newerSnowflake(left, right string) string {
	if snowflakeAfter(left, right) {
		return left
	}
	return right
}

func (m *Model) requestNotifications(cursor string, more, poll bool) tea.Cmd {
	if m.notificationPolling {
		return nil
	}
	m.notificationSeq++
	m.notificationPolling = true
	return fetchNotifications(m.requestContext(), cursor, more, poll, m.notificationSeq)
}

func (m *Model) scheduleNotificationPoll(delay time.Duration) tea.Cmd {
	if delay <= 0 {
		delay = notificationPollInterval
	}
	m.notificationTimerSeq++
	seq := m.notificationTimerSeq
	return tea.Tick(delay, func(time.Time) tea.Msg { return notificationPollTickMsg{seq: seq} })
}

func (m *Model) nextNotificationPollDelay(hadFresh bool) time.Duration {
	if hadFresh {
		m.notificationIdlePolls = 0
		return notificationPollInterval
	}
	m.notificationIdlePolls++
	delay := notificationPollInterval << min(m.notificationIdlePolls-1, 3)
	return min(delay, notificationMaxBackoff)
}

func (m *Model) notificationRetryDelay(err error) time.Duration {
	var limited *api.RateLimitError
	if errors.As(err, &limited) && !limited.Reset.IsZero() {
		return max(notificationPollInterval, time.Until(limited.Reset)+time.Second)
	}
	if m.notificationBackoff < notificationPollInterval {
		m.notificationBackoff = notificationPollInterval
	} else {
		m.notificationBackoff *= 2
		if m.notificationBackoff > notificationMaxBackoff {
			m.notificationBackoff = notificationMaxBackoff
		}
	}
	return m.notificationBackoff
}

func (m *Model) applyNotificationPage(msg notificationMsg) tea.Cmd {
	if msg.seq != m.notificationSeq {
		return nil
	}
	m.notificationPolling = false
	m.notificationLoading = false
	m.notificationMore = false
	if msg.err != nil {
		m.notificationErr = msg.err
		if msg.poll {
			return m.scheduleNotificationPoll(m.notificationRetryDelay(msg.err))
		}
		return nil
	}
	if msg.page == nil {
		m.notificationErr = errors.New("x returned no notifications page")
		return m.scheduleNotificationPoll(m.notificationRetryDelay(m.notificationErr))
	}
	m.notificationErr = nil
	m.notificationBackoff = 0
	if !m.notificationStateLoaded {
		m.notificationDeliveredID = msg.deliveredID
		m.notificationReadID = msg.readID
		m.notificationAccountID = msg.accountID
		m.notificationStateLoaded = true
		m.notificationBaselineSet = msg.deliveredID != ""
	}
	if m.notificationDeliveredID != "" {
		m.notificationBaselineSet = true
	}
	stateAccount := m.notificationAccountID
	stateDelivered := m.notificationDeliveredID
	stateRead := m.notificationReadID
	if msg.page.AccountID != "" && m.notificationAccountID != "" && msg.page.AccountID != m.notificationAccountID {
		m.notificationDeliveredID = ""
		m.notificationReadID = ""
		m.notifications = nil
		m.notificationQueue = nil
		m.notificationPopup = nil
		m.notificationBaselineSet = false
	}
	if msg.page.AccountID != "" {
		m.notificationAccountID = msg.page.AccountID
	}

	selectedID := ""
	if m.mode == modeNotifications && m.selected >= 0 && m.selected < len(m.notifications) {
		selectedID = m.notifications[m.selected].ID
	}
	var persist tea.Cmd
	hadFresh := false
	if msg.more {
		seen := make(map[string]bool, len(m.notifications))
		for _, item := range m.notifications {
			seen[item.ID] = true
		}
		for _, item := range msg.page.Notifications {
			if !seen[item.ID] && len(m.notifications) < notificationHistoryLimit {
				m.notifications = append(m.notifications, item)
				seen[item.ID] = true
			}
		}
		m.notificationCursor = msg.page.Cursor
		if len(m.notifications) >= notificationHistoryLimit {
			m.notificationCursor = ""
		}
	} else {
		oldDelivered := m.notificationDeliveredID
		newest := ""
		for _, item := range msg.page.Notifications {
			newest = newerSnowflake(item.ID, newest)
		}
		if !m.notificationBaselineSet {
			// Baseline existing history on first use; only future arrivals pop.
			m.notificationDeliveredID = newest
			if m.notificationReadID == "" {
				m.notificationReadID = newest
			}
			m.notificationBaselineSet = true
		} else {
			var fresh []api.Notification
			for _, item := range msg.page.Notifications {
				if snowflakeAfter(item.ID, oldDelivered) {
					fresh = append(fresh, item)
				}
			}
			hadFresh = len(fresh) > 0
			if m.mode == modeNotifications {
				m.notificationReadID = newerSnowflake(newest, m.notificationReadID)
			} else {
				m.enqueueNotifications(fresh)
			}
			m.notificationDeliveredID = newerSnowflake(newest, oldDelivered)
		}
		m.notificationCursor = msg.page.Cursor
		m.mergeNotificationsOnTop(msg.page.Notifications)
		if stateAccount != m.notificationAccountID || stateDelivered != m.notificationDeliveredID ||
			stateRead != m.notificationReadID || m.notificationStateDirty {
			persist = m.persistNotificationState()
		}
	}
	m.recountUnread()
	if m.mode == modeNotifications {
		if selectedID != "" {
			for i := range m.notifications {
				if m.notifications[i].ID == selectedID {
					m.selected = i
					m.notificationSelected = i
					break
				}
			}
		}
		m.syncViewport()
		m.ensureSelectedVisible()
	}
	var poll tea.Cmd
	if msg.poll {
		poll = m.scheduleNotificationPoll(m.nextNotificationPollDelay(hadFresh))
	}
	return tea.Batch(persist, poll, m.activateNotificationPopup())
}

func (m *Model) mergeNotificationsOnTop(in []api.Notification) {
	seen := make(map[string]bool, len(in)+len(m.notifications))
	merged := make([]api.Notification, 0, len(in)+len(m.notifications))
	for _, list := range [][]api.Notification{in, m.notifications} {
		for _, item := range list {
			if !seen[item.ID] {
				merged = append(merged, item)
				seen[item.ID] = true
			}
		}
	}
	if len(merged) > notificationHistoryLimit {
		merged = merged[:notificationHistoryLimit]
		m.notificationCursor = ""
	}
	m.notifications = merged
}

func (m *Model) enqueueNotifications(fresh []api.Notification) {
	// Keep the newest bounded set, then surface it oldest-first.
	limit := min(len(fresh), notificationPopupQueue)
	if len(fresh) > limit {
		m.notificationOverflow += len(fresh) - limit
	}
	for i := limit - 1; i >= 0; i-- {
		m.notificationQueue = append(m.notificationQueue, fresh[i])
	}
}

func (m *Model) activateNotificationPopup() tea.Cmd {
	if m.notificationPopup != nil || len(m.notificationQueue) == 0 || !m.canShowNotificationPopup() {
		return nil
	}
	item := m.notificationQueue[0]
	m.notificationQueue = m.notificationQueue[1:]
	m.notificationPopup = &item
	m.notificationPopupSeq++
	seq := m.notificationPopupSeq
	return tea.Tick(notificationPopupTime, func(time.Time) tea.Msg { return notificationPopupClearMsg{seq: seq} })
}

func (m Model) canShowNotificationPopup() bool {
	return (m.mode == modeFeed || m.mode == modeThread) && !m.help && !m.altText && !m.zoom
}

func (m *Model) dismissNotificationPopup() tea.Cmd {
	m.notificationPopup = nil
	m.notificationPopupSeq++
	return m.activateNotificationPopup()
}

func (m *Model) recountUnread() {
	count := 0
	for _, item := range m.notifications {
		if snowflakeAfter(item.ID, m.notificationReadID) {
			count++
		}
	}
	m.unreadNotifications = count
}

func (m Model) beginNotifications() (tea.Model, tea.Cmd) {
	if m.mode == modeNotifications {
		return m, nil
	}
	m.notificationReturn = m.mode
	m.notificationReturnSelected = m.selected
	if m.mode == modeFeed {
		m.feedSelected = m.selected
		m.notificationThread = nil
	} else if m.mode == modeThread {
		m.notificationThread = m.snapshotThread()
	}
	m.notificationPopup = nil
	m.notificationQueue = nil
	m.notificationOverflow = 0
	m.notificationPopupSeq++
	m.mode = modeNotifications
	m.selected = max(0, min(m.notificationSelected, len(m.notifications)-1))
	m.notificationErr = nil
	oldReadID := m.notificationReadID
	m.markNotificationsRead()
	m.syncViewport()
	m.ensureSelectedVisible()
	var fetch tea.Cmd
	if len(m.notifications) == 0 {
		m.notificationLoading = true
		if !m.notificationPolling {
			fetch = m.requestNotifications("", false, false)
		}
	}
	var persist tea.Cmd
	if oldReadID != m.notificationReadID || m.notificationStateDirty {
		persist = m.persistNotificationState()
	}
	return m, m.imageRepaint(tea.Batch(fetch, persist))
}

func (m *Model) markNotificationsRead() {
	for _, item := range m.notifications {
		m.notificationReadID = newerSnowflake(item.ID, m.notificationReadID)
	}
	m.recountUnread()
}

func (m Model) closeNotifications() (tea.Model, tea.Cmd) {
	m.notificationSelected = m.selected
	m.mode = m.notificationReturn
	m.selected = m.notificationReturnSelected
	if m.mode == modeThread {
		m.restoreThread(m.notificationThread)
		m.notificationThread = nil
	}
	m.syncViewport()
	m.ensureSelectedVisible()
	return m, m.imageRepaint(m.activateNotificationPopup())
}

func (m Model) applyThreadPageBehindNotifications(msg threadMsg) (tea.Model, tea.Cmd) {
	panelSelected := m.selected
	foreground := m.snapshotThread()
	m.restoreThread(m.notificationThread)
	m.selected = m.notificationReturnSelected
	next, cmd := m.applyThreadPage(msg, false)
	m = next.(Model)
	m.notificationReturnSelected = m.selected
	m.notificationThread = m.snapshotThread()
	if foreground.rootID == m.notificationThread.rootID && foreground.seq == msg.seq {
		foreground = m.notificationThread
	}
	m.restoreThread(foreground)
	m.selected = panelSelected
	return m, cmd
}

func (m Model) updateNotifications(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.notificationLoading || m.notificationMore {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case pageMsg:
		return m.applyFeedPage(msg)
	case threadMsg:
		if m.notificationReturn == modeThread {
			return m.applyThreadPageBehindNotifications(msg)
		}
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
	case "esc", "n":
		return m.closeNotifications()
	case "?", "f1":
		m.help = true
		return m, m.imageRepaint()
	case "j", "down":
		m.moveNotificationSelection(m.selected + 1)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMoreNotifications())
	case "k", "up":
		m.moveNotificationSelection(m.selected - 1)
		return m, m.imageRepaint(m.requestPreviews())
	case "g", "home":
		m.moveNotificationSelection(0)
		return m, m.imageRepaint(m.requestPreviews())
	case "G", "end":
		m.moveNotificationSelection(len(m.notifications) - 1)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMoreNotifications())
	case "a":
		if errors.Is(m.notificationErr, api.ErrSessionExpired) {
			m.action = Action{Kind: ActionAuthenticate}
			return m, tea.Quit
		}
	case "R", "ctrl+r":
		m.notificationLoading = true
		m.notificationErr = nil
		return m, m.imageRepaint(tea.Batch(m.spinner.Tick, m.requestNotifications("", false, false)))
	case "r":
		if post, ok := m.currentPost(); ok {
			return m.beginReply(post)
		}
	case "enter":
		if post, ok := m.currentPost(); ok {
			return m.beginThread(post, modeNotifications)
		}
	case " ", "e":
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
	case "v":
		return m, m.playSelectedVideo()
	case "l":
		return m, m.toggleSelectedLike()
	case "y":
		return m, m.copySelectedLink()
	}
	return m, nil
}

func (m *Model) moveNotificationSelection(target int) {
	if len(m.notifications) == 0 {
		return
	}
	m.selected = max(0, min(len(m.notifications)-1, target))
	m.notificationSelected = m.selected
	m.expanded = false
	m.syncViewport()
	m.ensureSelectedVisible()
}

func (m *Model) maybeLoadMoreNotifications() tea.Cmd {
	if len(m.notifications) > 0 && m.selected >= len(m.notifications)-5 && m.notificationCursor != "" && !m.notificationMore {
		m.notificationMore = true
		return m.requestNotifications(m.notificationCursor, true, false)
	}
	return nil
}

func (m Model) viewNotifications() string {
	footer := m.notificationFooter()
	if m.notificationLoading && len(m.notifications) == 0 {
		center := lipgloss.Place(m.viewport.Width, m.viewport.Height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(lavender).Render(m.spinner.View()+" checking notifications…"))
		return m.shell(center, footer)
	}
	if m.notificationErr != nil && len(m.notifications) == 0 {
		center := lipgloss.Place(m.viewport.Width, m.viewport.Height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(red).Width(max(20, m.viewport.Width-8)).Align(lipgloss.Center).
				Render("notifications are unavailable\n\n"+m.notificationErr.Error()))
		return m.shell(center, footer)
	}
	return m.shell(m.viewport.View(), footer)
}

func (m Model) notificationFooter() string {
	if m.toast != "" {
		return m.toast
	}
	if m.notificationErr != nil {
		if errors.Is(m.notificationErr, api.ErrSessionExpired) {
			return "a reconnect  ·  R retry  ·  esc back"
		}
		return "R retry  ·  esc back"
	}
	position := 0
	if len(m.notifications) > 0 {
		position = m.selected + 1
	}
	return fmt.Sprintf("%d/%d · r reply · enter conversation · esc back", position, len(m.notifications))
}

func (m Model) renderNotificationContent() (string, []int, []int) {
	if len(m.notifications) == 0 {
		return lipgloss.NewStyle().Foreground(muted).Width(m.contentWidth()).Align(lipgloss.Center).
			Render("all quiet · replies and mentions will appear here"), nil, nil
	}
	blocks := make([]string, 0, len(m.notifications))
	starts := make([]int, 0, len(m.notifications))
	ends := make([]int, 0, len(m.notifications))
	line := 0
	for i, item := range m.notifications {
		label := string(item.Kind)
		if snowflakeAfter(item.ID, m.notificationReadID) {
			label = "● " + label
		}
		badge := lipgloss.NewStyle().Foreground(lavender).Bold(true).Render(label)
		block := badge + "\n" + m.renderPost(item.Post, i == m.selected, abs(i-m.selected) <= inlineImageRadius, feedDepth)
		height := lipgloss.Height(block)
		starts = append(starts, line)
		ends = append(ends, line+height-1)
		blocks = append(blocks, block)
		line += height
		if i < len(m.notifications)-1 {
			line++
		}
	}
	return strings.Join(blocks, "\n\n"), starts, ends
}
