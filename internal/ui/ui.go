// Package ui holds the chrome xeet draws outside its full-screen interfaces:
// the wordmark, the mascot, and the handful of styles the commands share.
// Keeping them in one place is what makes `xeet auth`, `xeet theme`, and the
// composer look like the same program.
package ui

import (
	"os"

	"github.com/melqtx/xeet/internal/theme"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

// Logo is the wordmark shown when the terminal is wide enough to hold it.
const Logo = `██╗  ██╗███████╗███████╗████████╗
╚██╗██╔╝██╔════╝██╔════╝╚══██╔══╝
 ╚███╔╝ █████╗  █████╗     ██║
 ██╔██╗ ██╔══╝  ██╔══╝     ██║
██╔╝ ██╗███████╗███████╗   ██║
╚═╝  ╚═╝╚══════╝╚══════╝   ╚═╝`

// SmallLogo stands in for Logo in narrow terminals.
const SmallLogo = "x e e t  ✦"

// LogoMinWidth is the narrowest column the wordmark reads well in. The art
// itself is 32 columns; below this the small form takes over.
const LogoMinWidth = 40

// Mascot is the cat that keeps you company while xeet works.
const Mascot = ` /\_/\
( o.o )
 > ^ <`

// Styles is one palette resolved into the roles commands actually print:
// titles, the three result colors, and two grades of quieter text.
type Styles struct {
	Palette theme.Palette

	Title  lipgloss.Style
	Accent lipgloss.Style
	OK     lipgloss.Style
	Warn   lipgloss.Style
	Err    lipgloss.Style
	Body   lipgloss.Style
	Dim    lipgloss.Style
	Box    lipgloss.Style
}

// New resolves a palette into styles. Lipgloss drops the color codes on its own
// when stdout is not a terminal or NO_COLOR is set, so callers never have to
// branch on it.
func New(p theme.Palette) Styles {
	return Styles{
		Palette: p,
		Title:   lipgloss.NewStyle().Foreground(p.Blue).Bold(true),
		Accent:  lipgloss.NewStyle().Foreground(p.Pink),
		OK:      lipgloss.NewStyle().Foreground(p.Green),
		Warn:    lipgloss.NewStyle().Foreground(p.Yellow),
		Err:     lipgloss.NewStyle().Foreground(p.Red),
		Body:    lipgloss.NewStyle().Foreground(p.Bright),
		Dim:     lipgloss.NewStyle().Foreground(p.Muted),
		Box: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Lavender).
			Padding(1, 2),
	}
}

// Default is the styling used before any config is read.
func Default() Styles { return New(theme.Default()) }

// RenderLogo centers the wordmark in width, falling back to the small form when
// the terminal cannot hold it.
func (s Styles) RenderLogo(width int) string {
	logo := Logo
	if width < LogoMinWidth {
		logo = SmallLogo
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, s.Title.Render(logo))
}

// RenderMascot centers the cat above a caption, the way the composer does.
func (s Styles) RenderMascot(width int, caption string) string {
	cat := lipgloss.NewStyle().Foreground(s.Palette.Pink).Render(Mascot)
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, cat) + "\n" +
		lipgloss.PlaceHorizontal(width, lipgloss.Center, s.Dim.Render(caption+"  ✦"))
}

// Interactive reports whether xeet can run a picker: it needs a keyboard to
// read from and a screen to draw on, so a pipe on either end rules it out.
func Interactive() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}
