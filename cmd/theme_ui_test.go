package cmd

import (
	"strings"
	"testing"

	"github.com/melqtx/xeet/internal/theme"

	tea "github.com/charmbracelet/bubbletea"
)

func updatePicker(t *testing.T, p themePicker, msg tea.Msg) themePicker {
	t.Helper()
	next, _ := p.Update(msg)
	picker, ok := next.(themePicker)
	if !ok {
		t.Fatalf("Update returned %T, not a themePicker", next)
	}
	return picker
}

func key(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
}

func TestThemePickerStartsOnTheCurrentTheme(t *testing.T) {
	names := theme.Names()
	p := newThemePicker(names[len(names)-1])
	if p.names[p.cursor] != names[len(names)-1] {
		t.Fatalf("picker opened on %q, want the current theme %q", p.names[p.cursor], names[len(names)-1])
	}
}

func TestThemePickerEnterChoosesTheHighlightedTheme(t *testing.T) {
	p := newThemePicker(theme.Names()[0])
	p = updatePicker(t, p, key("j"))
	p = updatePicker(t, p, key("enter"))

	if p.chosen != theme.Names()[1] {
		t.Fatalf("chose %q, want %q", p.chosen, theme.Names()[1])
	}
	if !p.done {
		t.Error("picker should be done after enter")
	}
}

func TestThemePickerEscapeChoosesNothing(t *testing.T) {
	p := newThemePicker(theme.Names()[0])
	p = updatePicker(t, p, key("j"))
	p = updatePicker(t, p, key("esc"))

	if p.chosen != "" {
		t.Fatalf("escape saved %q; it should change nothing", p.chosen)
	}
}

func TestThemePickerStopsAtTheEnds(t *testing.T) {
	p := newThemePicker(theme.Names()[0])
	p.cursor = 0
	p = updatePicker(t, p, key("k"))
	if p.cursor != 0 {
		t.Errorf("cursor ran past the top: %d", p.cursor)
	}
	p = updatePicker(t, p, key("G"))
	p = updatePicker(t, p, key("j"))
	if p.cursor != len(p.names)-1 {
		t.Errorf("cursor ran past the bottom: %d", p.cursor)
	}
}

// The picker clears itself on the way out so the command can report the
// outcome only after the save has actually happened.
func TestThemePickerViewIsEmptyOnceDone(t *testing.T) {
	p := newThemePicker(theme.Names()[0])
	if p.View() == "" {
		t.Fatal("an open picker should draw something")
	}
	p = updatePicker(t, p, key("enter"))
	if p.View() != "" {
		t.Error("a finished picker should leave nothing behind")
	}
}

func TestThemePickerPreviewFollowsTheHighlightedPalette(t *testing.T) {
	p := newThemePicker(theme.Names()[0])
	first := p.palette()
	p = updatePicker(t, p, key("j"))
	if p.palette() == first {
		t.Fatal("moving the cursor did not change the previewed palette")
	}
	if !strings.Contains(p.View(), "enter to xeet") {
		t.Error("the preview should show the composer footer")
	}
}
