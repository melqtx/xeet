package timeline

import (
	"strings"
	"testing"

	"github.com/melqtx/xeet/internal/theme"
	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func twoAccounts() []config.AccountInfo {
	return []config.AccountInfo{
		{UserID: "42", Handle: "alice", Active: true},
		{UserID: "84", Handle: "bob"},
	}
}

func TestColumnAddFullFlowThroughPickers(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(repeatedColumnSpecs(1, FeedForYou, "", ""))
	m.columns[0].loading = false
	m.accounts = twoAccounts()

	m = update(t, m, runeKey('n'))
	if m.mode != modeChoicePicker || m.picker.intent != intentColumnKind {
		t.Fatalf("n opened mode=%v intent=%v, want the kind picker", m.mode, m.picker.intent)
	}

	m = update(t, m, runeKey('j'))
	m = update(t, m, runeKey('j')) // bookmarks
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeChoicePicker || m.picker.intent != intentColumnAccount {
		t.Fatalf("kind enter landed on mode=%v intent=%v, want the account picker", m.mode, m.picker.intent)
	}
	if m.columnDraft.Kind != FeedBookmarks {
		t.Fatalf("draft kind = %v, want bookmarks", m.columnDraft.Kind)
	}
	if m.picker.items[0].value != "" {
		t.Error("account picker's first entry should follow the active account (empty id)")
	}

	m = update(t, m, runeKey('j')) // @alice
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeFeed {
		t.Fatalf("mode = %v after the final pick, want the feed", m.mode)
	}
	if len(m.columns) != 2 {
		t.Fatalf("columns = %d, want the draft added as a second column", len(m.columns))
	}
	added := m.columns[1]
	if added.feed != FeedBookmarks || added.accountID != "42" {
		t.Fatalf("added column = {feed:%v account:%q}, want bookmarks on 42", added.feed, added.accountID)
	}
	if m.columnDraft != (ColumnSpec{}) {
		t.Error("draft was not cleared after the column was added")
	}
}

func TestColumnAddEscapeDiscardsDraft(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(repeatedColumnSpecs(1, FeedForYou, "", ""))
	m.columns[0].loading = false
	m.accounts = twoAccounts()

	m = update(t, m, runeKey('n'))
	m = update(t, m, runeKey('j')) // following
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.picker.intent != intentColumnAccount {
		t.Fatalf("intent = %v, want the account stage", m.picker.intent)
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeFeed {
		t.Fatalf("mode = %v after esc, want the feed", m.mode)
	}
	if len(m.columns) != 1 {
		t.Error("esc at the account stage still added the column")
	}
	if m.columnDraft != (ColumnSpec{}) {
		t.Error("esc left a draft behind")
	}
}

func TestColumnAddSearchFlowDoesNotApplyOrQuit(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(repeatedColumnSpecs(1, FeedForYou, "", ""))
	m.columns[0].loading = false
	m.accounts = twoAccounts()

	m = update(t, m, runeKey('n'))
	m.picker.sel = 4 // search
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeSearch || m.searchFor != targetNewColumn {
		t.Fatalf("search kind landed on mode=%v target=%v, want a new-column search", m.mode, m.searchFor)
	}

	m.searchInput.SetValue("terminal cats")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeChoicePicker || m.picker.intent != intentColumnAccount {
		t.Fatalf("search enter landed on mode=%v, want the account picker", m.mode)
	}
	if m.columnDraft.Kind != FeedSearch || m.columnDraft.Query != "terminal cats" {
		t.Fatalf("draft = %+v, want the search on terminal cats", m.columnDraft)
	}
	if m.columns[0].feed == FeedSearch {
		t.Error("the new-column search overwrote the focused column's feed")
	}

	// Escape mid-flow abandons the draft instead of quitting like `xeet search`.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeFeed || len(m.columns) != 1 {
		t.Fatalf("esc left mode=%v columns=%d, want the feed with one column", m.mode, len(m.columns))
	}
}

func TestColumnAddSearchEscapeSkipsTheQuitSpecialCase(t *testing.T) {
	// A direct `xeet search` session has an empty search column; escape in the
	// add flow must still cancel rather than quit.
	m := NewWithImageMode("off")
	m.configureColumns([]ColumnSpec{{Kind: FeedSearch}})
	m.cur().loading = false
	m.accounts = twoAccounts()

	m = update(t, m, runeKey('n'))
	m.picker.sel = 4
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeFeed {
		t.Fatalf("esc in the add-flow search left mode=%v, want the feed (not a quit)", m.mode)
	}
	if m.searchFor != targetFocusedColumn {
		t.Error("searchFor was not reset by the cancel")
	}
}

func TestColumnAddListFlowUsesActiveAccount(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns([]ColumnSpec{{Kind: FeedForYou, AccountID: "42"}})
	m.columns[0].loading = false
	m.accounts = twoAccounts()

	m = update(t, m, runeKey('n'))
	m.picker.sel = 3 // list
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeListPicker || m.listFor != targetNewColumn {
		t.Fatalf("list kind landed on mode=%v target=%v, want a new-column list pick", m.mode, m.listFor)
	}

	// A pick destined for another column's account must not fill this picker.
	m = update(t, m, listsMsg{picker: true, accountID: "84", lists: []api.ListInfo{{ID: "9", Name: "wrong"}}})
	if len(m.listPicker) != 0 {
		t.Error("the picker accepted lists fetched for a specific column account")
	}
	m = update(t, m, listsMsg{picker: true, accountID: "", lists: []api.ListInfo{{ID: "7", Name: "Frens"}}})
	if len(m.listPicker) != 1 {
		t.Fatalf("listPicker = %v, want the active account's lists", m.listPicker)
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeChoicePicker || m.picker.intent != intentColumnAccount {
		t.Fatalf("list enter landed on mode=%v, want the account picker", m.mode)
	}
	if m.columnDraft.Kind != FeedList || m.columnDraft.ListID != "7" {
		t.Fatalf("draft = %+v, want list 7", m.columnDraft)
	}
}

func TestColumnAccountRetargetFlow(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns([]ColumnSpec{{Kind: FeedForYou, AccountID: "42"}})
	m.columns[0].loading = false
	m.accounts = twoAccounts()

	m = update(t, m, runeKey('s'))
	if m.mode != modeChoicePicker || m.picker.intent != intentColumnAccountRetarget {
		t.Fatalf("s opened mode=%v intent=%v, want the retarget picker", m.mode, m.picker.intent)
	}
	m = update(t, m, runeKey('j'))
	m = update(t, m, runeKey('j')) // @bob
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.cur().accountID != "84" {
		t.Fatalf("accountID = %q, want 84", m.cur().accountID)
	}
}

func TestAccountPickerRefreshesItemsOnLoad(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(repeatedColumnSpecs(1, FeedForYou, "", ""))
	m.accounts = twoAccounts()
	m.beginAccountPicker("column account", intentColumnAccountRetarget)
	before := len(m.picker.items)

	m = update(t, m, accountsLoadedMsg{accounts: []config.AccountInfo{
		{UserID: "42", Handle: "alice", Active: true},
	}})

	if len(m.picker.items) != before-1 {
		t.Fatalf("items = %d, want %d after an account vanished", len(m.picker.items), before-1)
	}
}

func TestRemoveColumnKey(t *testing.T) {
	m := modelWithNamedColumns("one", "two")
	m = update(t, m, runeKey('x'))
	if len(m.columns) != 1 {
		t.Fatalf("x left %d columns, want 1", len(m.columns))
	}
}

func TestSetImageModeEvictsPreviewsAndToasts(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(repeatedColumnSpecs(1, FeedForYou, "", ""))
	m.previews["p1"] = previewState{content: "old ansi art"}

	cmd := m.setImageMode(imageModeANSI)

	if cmd == nil {
		t.Fatal("setImageMode returned no command; nothing would repaint")
	}
	if m.imageMode != imageModeANSI {
		t.Fatalf("imageMode = %q, want ansi", m.imageMode)
	}
	if len(m.previews) != 0 {
		t.Error("stale previews survived the renderer switch")
	}
	if !strings.Contains(m.toast, "ansi") {
		t.Fatalf("toast = %q, want the new mode named", m.toast)
	}
}

func TestSetImageModeMultiColumnFallsBack(t *testing.T) {
	m := modelWithNamedColumns("one", "two")

	m.setImageMode(imageModeWezTerm)

	if m.imageMode != imageModeANSI {
		t.Fatalf("imageMode = %q, want the ansi fallback for multiple columns", m.imageMode)
	}
	if m.imageNote != multiColumnImageNote {
		t.Fatalf("imageNote = %q, want the multi-column explanation", m.imageNote)
	}
}

func TestStorePreviewDropsMismatchedMode(t *testing.T) {
	m := NewWithImageMode("off")
	m.configureColumns(repeatedColumnSpecs(1, FeedForYou, "", ""))
	m.imageMode = imageModeANSI
	colID := m.cur().id

	m.storePreview(previewMsg{postID: "stale", colID: colID, mode: imageModeWezTerm, nativeData: "png"})
	if _, ok := m.previews["stale"]; ok {
		t.Error("a preview from the previous renderer was stored")
	}
	m.storePreview(previewMsg{postID: "fresh", colID: colID, mode: imageModeANSI, content: "art"})
	if _, ok := m.previews["fresh"]; !ok {
		t.Error("a preview for the current renderer was dropped")
	}
}

type fakeThemeSaver struct {
	saved []string
	err   error
}

func (f *fakeThemeSaver) SaveTheme(name string) error {
	f.saved = append(f.saved, name)
	return f.err
}

func TestSetThemeAppliesPaletteAndSaves(t *testing.T) {
	saver := &fakeThemeSaver{}
	previous := openThemeSaver
	openThemeSaver = func() (themeSaver, error) { return saver, nil }
	t.Cleanup(func() { openThemeSaver = previous })

	m := NewWithImageMode("off")
	m.configureColumns(repeatedColumnSpecs(1, FeedForYou, "", ""))
	before := blue

	name := activeThemeName
	for _, candidate := range theme.Names() {
		if candidate != activeThemeName {
			name = candidate
			break
		}
	}
	m.setTheme(name)

	if blue == before {
		t.Error("the palette colors did not change")
	}
	if activeThemeName != name {
		t.Fatalf("activeThemeName = %q, want %q", activeThemeName, name)
	}
	msg := saveThemeCmd(name)()
	if msg != nil {
		t.Fatalf("saveThemeCmd returned %v, want nil on success", msg)
	}
	if len(saver.saved) != 1 || saver.saved[0] != name {
		t.Fatalf("saved = %v, want [%s]", saver.saved, name)
	}
	t.Cleanup(func() { ApplyTheme(theme.Default()) })

	m.setTheme("no-such-theme")
	if !strings.Contains(m.toast, "unknown theme") {
		t.Fatalf("toast = %q, want an unknown-theme notice", m.toast)
	}
}

func TestHelpListsColumnAndDisplayKeys(t *testing.T) {
	m := NewWithImageMode("off")
	m.width, m.height = 100, 40
	view := m.viewHelp()
	for _, want := range []string{"add column", "remove column", "column account", "image mode", "theme"} {
		if !strings.Contains(view, want) {
			t.Errorf("help is missing %q", want)
		}
	}
}
