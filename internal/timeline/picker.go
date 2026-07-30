package timeline

import (
	"fmt"

	"github.com/melqtx/xeet/internal/theme"
	"github.com/melqtx/xeet/internal/tui"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// pickerTarget tells the search and list pickers whether their result edits
// the focused column (the historical behavior) or fills the draft of a column
// being added. The zero value keeps every existing call site unchanged.
type pickerTarget int

const (
	targetFocusedColumn pickerTarget = iota
	targetNewColumn
)

// choiceIntent identifies what a choicePicker selection means, so one picker
// implementation can back column kinds, accounts, image modes, and themes.
type choiceIntent int

const (
	intentColumnKind choiceIntent = iota
	intentColumnAccount
	intentColumnAccountRetarget
	intentImageMode
	intentTheme
)

type choiceItem struct {
	label string
	hint  string
	value string
}

type choicePicker struct {
	title  string
	items  []choiceItem
	sel    int
	intent choiceIntent
}

// beginColumnAdd starts the kind -> detail -> account flow that ends in
// addColumn. The draft accumulates one decision per stage; escape at any
// stage throws the whole draft away.
func (m *Model) beginColumnAdd() tea.Cmd {
	if len(m.columns) >= maxColumns {
		return m.showToast("already at 4 columns")
	}
	m.columnDraft = ColumnSpec{}
	m.beginChoicePicker("new column · feed", []choiceItem{
		{label: "for you", hint: "the recommended timeline", value: "foryou"},
		{label: "following", hint: "accounts you follow", value: "following"},
		{label: "bookmarks", hint: "your saved posts", value: "bookmarks"},
		{label: "list", hint: "pick one of your lists next", value: "list"},
		{label: "search", hint: "type a query next", value: "search"},
		// Appended last on purpose: picker tests index these items by position.
		{label: "notifications", hint: "likes, reposts, and replies to you", value: "notifications"},
	}, intentColumnKind)
	return m.imageRepaint()
}

func (m *Model) beginChoicePicker(title string, items []choiceItem, intent choiceIntent) {
	m.picker = choicePicker{title: title, items: items, intent: intent}
	m.mode = modeChoicePicker
}

// beginAccountPicker offers every saved account plus an empty-id entry that
// follows the active account. Unlike the @ key it never calls SetActive: a
// per-column account is session state, not a config change.
func (m *Model) beginAccountPicker(title string, intent choiceIntent) tea.Cmd {
	if len(m.accounts) == 0 {
		return m.showToast("no saved accounts; run 'xeet auth' first")
	}
	m.beginChoicePicker(title, m.accountChoiceItems(), intent)
	return m.imageRepaint(loadAccountsCmd())
}

func (m Model) accountChoiceItems() []choiceItem {
	items := []choiceItem{{
		label: "active account",
		hint:  "follows the @ key",
		value: "",
	}}
	for _, account := range m.accounts {
		hint := account.UserID
		if account.Active {
			hint += " · active"
		}
		items = append(items, choiceItem{
			label: accountInfoLabel(account),
			hint:  hint,
			value: account.UserID,
		})
	}
	return items
}

func (m Model) updateChoicePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case listsMsg:
		if !msg.picker && msg.err == nil {
			m.nameListColumns(msg.accountID, msg.lists)
		}
		return m, nil
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
		if m.columnsLoading() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc":
		return m.cancelChoicePicker()
	case "j", "down":
		m.picker.sel = min(max(0, len(m.picker.items)-1), m.picker.sel+1)
	case "k", "up":
		m.picker.sel = max(0, m.picker.sel-1)
	case "enter":
		if m.picker.sel < 0 || m.picker.sel >= len(m.picker.items) {
			return m, nil
		}
		return m.applyChoice(m.picker.items[m.picker.sel].value)
	}
	return m, nil
}

func (m Model) applyChoice(value string) (tea.Model, tea.Cmd) {
	switch m.picker.intent {
	case intentColumnKind:
		switch value {
		case "list":
			m.listFor = targetNewColumn
			return m, m.beginListPicker()
		case "search":
			m.searchFor = targetNewColumn
			return m, m.beginSearch()
		case "following":
			m.columnDraft.Kind = FeedFollowing
		case "bookmarks":
			m.columnDraft.Kind = FeedBookmarks
		case "notifications":
			m.columnDraft.Kind = FeedNotifications
		default:
			m.columnDraft.Kind = FeedForYou
		}
		return m, m.beginAccountPicker("new column · account", intentColumnAccount)
	case intentColumnAccount:
		m.columnDraft.AccountID = value
		draft := m.columnDraft
		m.columnDraft = ColumnSpec{}
		m.mode = modeFeed
		return m, m.addColumn(draft)
	case intentColumnAccountRetarget:
		m.mode = modeFeed
		return m, m.setColumnAccount(value)
	case intentImageMode:
		m.mode = modeFeed
		return m, m.setImageMode(imageMode(value))
	case intentTheme:
		m.mode = modeFeed
		return m, m.setTheme(value)
	}
	return m, nil
}

func (m Model) cancelChoicePicker() (tea.Model, tea.Cmd) {
	// Mid-add-flow intents abandon the draft; retarget/display intents have no
	// draft to lose.
	if m.picker.intent == intentColumnKind || m.picker.intent == intentColumnAccount {
		m.columnDraft = ColumnSpec{}
	}
	m.mode = modeFeed
	m.syncViewport()
	m.ensureSelectedVisible()
	return m, m.imageRepaint(m.requestPreviews())
}

func (m Model) imageModeItems() []choiceItem {
	detected, note := resolveImageMode("auto")
	items := []choiceItem{
		{label: "off", hint: "no image previews", value: string(imageModeOff)},
		{label: "ansi", hint: "half-block pixels, works everywhere", value: string(imageModeANSI)},
	}
	if detected == imageModeNative || detected == imageModeWezTerm {
		items = append(items, choiceItem{
			label: string(detected),
			hint:  "native graphics for this terminal",
			value: string(detected),
		})
	} else if note != "" {
		items[1].hint = note
	}
	for i := range items {
		if items[i].value == string(m.imageMode) {
			items[i].hint += " · current"
		}
	}
	return items
}

// setImageMode swaps the renderer mid-session. Cached previews hold payloads
// in the old renderer's format, so every entry is dropped; stragglers still
// downloading for the old mode are rejected by the mode guard in storePreview.
func (m *Model) setImageMode(mode imageMode) tea.Cmd {
	if mode == m.imageMode {
		return m.showToast("images already " + string(mode))
	}
	previous := m.imageMode
	m.imageMode = mode
	m.imageNote = ""
	m.enforceMultiColumnImageMode()
	for id, preview := range m.previews {
		m.evictPreview(id, preview)
	}
	m.resize()
	cmds := []tea.Cmd{m.requestPreviews(), m.showToast("images: " + string(m.imageMode))}
	if previous == imageModeNative || previous == imageModeWezTerm {
		// Pixel graphics leave residue the line diff cannot erase.
		cmds = append(cmds, func() tea.Msg { return tea.ClearScreen() })
	}
	return m.imageRepaint(cmds...)
}

// activeThemeName tracks the live palette so the picker can mark the current
// entry; cmd sets it at startup from the flag or config.
var activeThemeName = theme.DefaultName

func SetThemeName(name string) { activeThemeName = name }

func (m Model) themeItems() []choiceItem {
	names := theme.Names()
	items := make([]choiceItem, 0, len(names))
	for _, name := range names {
		item := choiceItem{label: name, value: name}
		if name == activeThemeName {
			item.hint = "current"
		}
		items = append(items, item)
	}
	return items
}

// setTheme recolors immediately -- every style is rebuilt per frame from the
// package-level colors -- and saves the choice like `xeet theme` does.
func (m *Model) setTheme(name string) tea.Cmd {
	palette, ok := theme.Named(name)
	if !ok {
		return m.showToast("unknown theme " + name)
	}
	ApplyTheme(palette)
	tui.ApplyTheme(palette)
	activeThemeName = name
	return m.imageRepaint(m.showToast("theme: "+name), saveThemeCmd(name))
}

type themeSaver interface{ SaveTheme(string) error }

var openThemeSaver = func() (themeSaver, error) {
	return config.NewConfigManager()
}

func saveThemeCmd(name string) tea.Cmd {
	return func() tea.Msg {
		saver, err := openThemeSaver()
		if err != nil {
			return actionMsg{err: fmt.Errorf("save theme: %w", err)}
		}
		if err := saver.SaveTheme(name); err != nil {
			return actionMsg{err: fmt.Errorf("save theme: %w", err)}
		}
		return nil
	}
}
