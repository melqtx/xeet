package ui

import (
	"strings"
	"testing"

	"github.com/melqtx/xeet/internal/theme"
)

func TestRenderLogoShrinksForNarrowTerminals(t *testing.T) {
	s := Default()
	if !strings.Contains(s.RenderLogo(LogoMinWidth), "█") {
		t.Error("a terminal wide enough should get the wordmark")
	}
	narrow := s.RenderLogo(LogoMinWidth - 1)
	if strings.Contains(narrow, "█") {
		t.Errorf("a narrow terminal should get the small logo, got:\n%s", narrow)
	}
	if !strings.Contains(narrow, "x e e t") {
		t.Errorf("the small logo should still say xeet, got:\n%s", narrow)
	}
}

func TestRenderMascotKeepsTheCaption(t *testing.T) {
	out := Default().RenderMascot(40, "ready when you are")
	for _, want := range []string{"o.o", "ready when you are"} {
		if !strings.Contains(out, want) {
			t.Errorf("mascot output is missing %q:\n%s", want, out)
		}
	}
}

func TestNewCarriesThePaletteThrough(t *testing.T) {
	nord, ok := theme.Named("nord")
	if !ok {
		t.Fatal("nord should be a preset")
	}
	if New(nord).Palette != nord {
		t.Error("styles should keep the palette they were built from")
	}
}
