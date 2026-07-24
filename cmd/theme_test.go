package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/melqtx/xeet/internal/theme"
	"github.com/melqtx/xeet/internal/ui"
)

func TestPrintThemesMarksTheCurrentOne(t *testing.T) {
	var buf bytes.Buffer
	printThemes(&buf, ui.Default(), "nord")
	out := buf.String()

	for _, name := range theme.Names() {
		if !strings.Contains(out, name) {
			t.Errorf("the list is missing %q", name)
		}
	}
	if !strings.Contains(out, "* nord") {
		t.Errorf("nord is not marked as current:\n%s", out)
	}
	if strings.Contains(out, "* gruvbox") {
		t.Errorf("a theme that is not current should not be marked:\n%s", out)
	}
}

func TestUnknownThemeErrorNamesTheAlternatives(t *testing.T) {
	err := unknownThemeError("neon")
	for _, want := range []string{"neon", "nord", "tokyonight"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestPaletteNamedFallsBackToTheDefault(t *testing.T) {
	if paletteNamed("no-such-theme") != theme.Default() {
		t.Error("an unreadable theme name should still leave xeet drawable")
	}
	nord, _ := theme.Named("nord")
	if paletteNamed("nord") != nord {
		t.Error("a known name should resolve to its own palette")
	}
}

func TestApplyConfiguredThemeRejectsUnknownNames(t *testing.T) {
	if err := applyConfiguredTheme("neon"); err == nil {
		t.Fatal("an unknown --theme should fail rather than silently fall back")
	}
	if err := applyConfiguredTheme("nord"); err != nil {
		t.Fatalf("applying a known theme failed: %v", err)
	}
}
