package cmd

import (
	"context"
	"errors"

	"github.com/melqtx/xeet/internal/theme"
	"github.com/melqtx/xeet/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// themePicker walks the palettes with a live preview. Everything on screen —
// the list, the sample posts, the picker's own chrome — is drawn in the
// highlighted palette, because seeing a theme is the only honest way to pick
// one.
type themePicker struct {
	names   []string
	current string
	cursor  int
	width   int
	chosen  string
	done    bool
}

func newThemePicker(current string) themePicker {
	p := themePicker{names: theme.Names(), current: current, width: 80}
	for i, name := range p.names {
		if name == current {
			p.cursor = i
		}
	}
	return p
}

func (p themePicker) Init() tea.Cmd { return nil }

func (p themePicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.names)-1 {
				p.cursor++
			}
		case "g", "home":
			p.cursor = 0
		case "G", "end":
			p.cursor = len(p.names) - 1
		case "enter":
			p.chosen = p.names[p.cursor]
			p.done = true
			return p, tea.Quit
		case "esc", "q", "ctrl+c":
			p.done = true
			return p, tea.Quit
		}
	}
	return p, nil
}

func (p themePicker) palette() theme.Palette {
	if palette, ok := theme.Named(p.names[p.cursor]); ok {
		return palette
	}
	return theme.Default()
}

// View leaves nothing behind once a choice is made: the command prints the
// outcome itself, after the save has actually happened.
func (p themePicker) View() string {
	if p.done {
		return ""
	}
	s := ui.New(p.palette())
	body := lipgloss.JoinHorizontal(lipgloss.Top, p.renderList(s), "  ", p.renderPreview(s))
	if p.width < 74 {
		body = lipgloss.JoinVertical(lipgloss.Left, p.renderList(s), "", p.renderPreview(s))
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		s.Accent.Bold(true).Render("pick a theme  ✦"),
		"",
		body,
		"",
		s.Dim.Render("↑↓ / j k  move   ·   enter  save as default   ·   esc  keep "+p.current),
	) + "\n"
}

func (p themePicker) renderList(s ui.Styles) string {
	rows := make([]string, 0, len(p.names))
	for i, name := range p.names {
		label := name
		if name == p.current {
			label += "  (default)"
		}
		if i == p.cursor {
			rows = append(rows, s.Accent.Render("› ")+s.Body.Bold(true).Render(label))
			continue
		}
		rows = append(rows, s.Dim.Render("  "+label))
	}
	return lipgloss.NewStyle().Width(24).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

// renderPreview mirrors what the timeline and composer actually draw: a
// selected post, a neighbouring one, and the composer's footer. Every color
// role appears at least once, so no palette hides a surprise.
func (p themePicker) renderPreview(s ui.Styles) string {
	c := s.Palette
	paint := func(color lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(color) }

	swatch := ""
	for _, color := range []lipgloss.Color{c.Blue, c.Lavender, c.Pink, c.Green, c.Yellow, c.Red, c.Bright, c.Dim, c.Muted} {
		// A gap between the blocks; two neighbouring roles in a subtle palette
		// otherwise merge into one bar.
		swatch += paint(color).Render("███") + " "
	}

	selected := paint(c.Blue).Render("▎") + " " +
		paint(c.Bright).Bold(true).Render("melqtx") + "  " +
		paint(c.Blue).Render("@melqtx") + paint(c.Muted).Render(" · 2h") + "\n" +
		"    " + paint(c.Bright).Render("post to x from your terminal.") + "\n" +
		"    " + paint(c.Pink).Render("♥ 42") + paint(c.Muted).Render("   ↺ 7   ") +
		paint(c.Blue).Underline(true).Render("x.com/melqtx")

	neighbour := "  " + paint(c.Dim).Bold(true).Render("the cat") + "  " +
		paint(c.Muted).Render("@catsofxeet · 4h") + "\n" +
		"    " + paint(c.Dim).Render("ready when you are") + "\n" +
		"    " + paint(c.Muted).Render("♡ 3   ↺ 1")

	status := paint(c.Muted).Render("12/280") + paint(c.Pink).Render("  ·  enter to xeet") + "\n" +
		paint(c.Green).Render("✓ posted") + paint(c.Muted).Render("   ") +
		paint(c.Yellow).Render("⚠ rate limited") + paint(c.Muted).Render("   ") +
		paint(c.Red).Render("✗ failed")

	return s.Box.Width(44).Render(lipgloss.JoinVertical(lipgloss.Left,
		swatch, "", selected, "", neighbour, "", status))
}

// runThemePicker returns the chosen theme, or "" when the user backed out.
func runThemePicker(ctx context.Context, current string) (string, error) {
	final, err := tea.NewProgram(newThemePicker(current), tea.WithContext(ctx)).Run()
	if err != nil {
		// An interrupt reached the program through ctx; nothing was saved, so
		// this is an ordinary "never mind".
		if errors.Is(err, tea.ErrProgramKilled) {
			return "", nil
		}
		return "", err
	}
	picker, ok := final.(themePicker)
	if !ok {
		return "", nil
	}
	return picker.chosen, nil
}
