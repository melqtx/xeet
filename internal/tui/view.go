package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/melqtx/xeet/internal/media"
	"github.com/melqtx/xeet/internal/theme"
	"github.com/melqtx/xeet/pkg/api"

	"github.com/charmbracelet/lipgloss"
)

var (
	blue     lipgloss.Color
	lavender lipgloss.Color
	pink     lipgloss.Color
	muted    lipgloss.Color
	green    lipgloss.Color
	red      lipgloss.Color

	dialogStyle lipgloss.Style
)

func init() { ApplyTheme(theme.Default()) }

// ApplyTheme recolors the composer. Call it before Run; layout is unaffected.
func ApplyTheme(p theme.Palette) {
	blue, lavender, pink = p.Blue, p.Lavender, p.Pink
	muted, green, red = p.Muted, p.Green, p.Red
	dialogStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lavender).
		Padding(1, 2)
}

const xeetLogo = `██╗  ██╗███████╗███████╗████████╗
╚██╗██╔╝██╔════╝██╔════╝╚══██╔══╝
 ╚███╔╝ █████╗  █████╗     ██║
 ██╔██╗ ██╔══╝  ██╔══╝     ██║
██╔╝ ██╗███████╗███████╗   ██║
╚═╝  ╚═╝╚══════╝╚══════╝   ╚═╝`

func (m Model) View() string {
	if m.width <= 0 {
		return "warming up xeet…"
	}
	if m.dialog != dialogNone {
		return m.viewDialog()
	}

	var content string
	switch m.screen {
	case screenPosting:
		content = m.viewPosting()
	case screenSuccess:
		content = m.viewSuccess()
	default:
		content = m.viewComposer()
	}
	return content
}

func (m Model) contentWidth() int {
	w := m.width - 4
	if w > 72 {
		w = 72
	}
	if w < 28 {
		w = 28
	}
	return w
}

func (m Model) viewComposer() string {
	w := m.contentWidth()
	logo := m.renderLogo(w)
	mascot := m.renderMascot(w, "ready when you are")

	border := lavender
	if m.focus == focusEditor {
		border = blue
	}
	editor := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(w - 4).
		Render(m.editor.View())

	rows := []string{logo, mascot, "", editor}
	if len(m.attachments) > 0 {
		rows = append(rows, "", m.viewAttachments(w))
	}
	if m.lastErr != nil {
		rows = append(rows, "", lipgloss.NewStyle().Foreground(red).Width(w).Render("oops: "+m.lastErr.Error()))
		if canOpenDraftInX(m.lastErr) && strings.TrimSpace(m.editor.Value()) != "" {
			hint := "b open text in X"
			var ambiguous *api.AmbiguousPostError
			if errors.As(m.lastErr, &ambiguous) {
				hint = "check profile, then b open text in X"
			}
			rows = append(rows, lipgloss.NewStyle().Foreground(pink).Width(w).Render(hint))
		}
		if m.postDiagnostic != "" {
			rows = append(rows, lipgloss.NewStyle().Foreground(muted).Width(w).Render("details: "+m.postDiagnostic))
		}
	} else if m.toast != "" {
		rows = append(rows, "", lipgloss.NewStyle().Foreground(muted).Width(w).Render("✦ "+m.toast))
	}

	countColor := muted
	if m.editor.Length() >= 260 {
		countColor = pink
	}
	status := lipgloss.NewStyle().Foreground(countColor).Render(fmt.Sprintf("%d/280", m.editor.Length()))
	if len(m.attachments) > 0 {
		status += lipgloss.NewStyle().Foreground(muted).Render(fmt.Sprintf("  •  %d pic%s", len(m.attachments), plural(len(m.attachments))))
	}
	status += lipgloss.NewStyle().Foreground(pink).Render("  •  enter to xeet")

	rows = append(rows, "", status)

	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) renderLogo(width int) string {
	if width < 40 {
		logo := lipgloss.NewStyle().Foreground(blue).Bold(true).Render("x e e t  ✦")
		return lipgloss.PlaceHorizontal(width, lipgloss.Center, logo)
	}
	logo := lipgloss.NewStyle().Foreground(blue).Bold(true).Render(xeetLogo)
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, logo)
}

func (m Model) renderMascot(width int, message string) string {
	cat := lipgloss.NewStyle().Foreground(pink).Render(` /\_/\
( o.o )
 > ^ <`)
	caption := lipgloss.NewStyle().Foreground(muted).Render(message + "  ✦")
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, cat) + "\n" +
		lipgloss.PlaceHorizontal(width, lipgloss.Center, caption)
}

func (m Model) viewAttachments(width int) string {
	rows := []string{lipgloss.NewStyle().Foreground(pink).Render(fmt.Sprintf("pics  %d/%d", len(m.attachments), media.MaxAttachments))}
	for i, a := range m.attachments {
		prefix := "  ◦ "
		style := lipgloss.NewStyle().Foreground(muted)
		if m.focus == focusAttachments && i == m.selected {
			prefix = "  › "
			style = style.Foreground(blue)
		}
		available := width - 28
		if available < 8 {
			available = 8
		}
		name := truncateRunes(a.Name, available)
		meta := fmt.Sprintf("%s · %dx%d · %s", a.Format, a.Width, a.Height, media.HumanBytes(int(a.Size)))
		if width < 52 || a.IsVideo() {
			meta = fmt.Sprintf("%s · %s", a.Format, media.HumanBytes(int(a.Size)))
		}
		rows = append(rows, style.Render(prefix+name+"  "+meta))
	}
	if m.focus == focusAttachments {
		rows = append(rows, lipgloss.NewStyle().Foreground(muted).Render("    arrows pick  •  delete remove  •  tab back"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m Model) viewPosting() string {
	stage := "getting things ready…"
	if m.cancelling {
		stage = "bringing it back…"
	} else {
		switch m.postStage.Stage {
		case api.PostStageUploading:
			stage = fmt.Sprintf("sending pic %d of %d…", m.postStage.Current, m.postStage.Total)
			if m.postStage.TotalBytes > 0 {
				percent := m.postStage.TransferredBytes * 100 / m.postStage.TotalBytes
				stage = fmt.Sprintf("sending %s… %d%%", m.postStage.Name, percent)
			}
		case api.PostStageProcessing:
			stage = "X is processing your video…"
		case api.PostStageDiscovering:
			stage = "finding the way to X…"
		case api.PostStagePublishing:
			stage = "sending your xeet…"
		case api.PostStageReconciling:
			stage = "checking whether it landed…"
		}
	}
	body := m.renderLogo(m.contentWidth()) + "\n" +
		m.renderMascot(m.contentWidth(), "hold on tight") + "\n\n" +
		lipgloss.NewStyle().Foreground(pink).Bold(true).Width(m.contentWidth()).Align(lipgloss.Center).Render(m.spinner.View()+" "+stage) +
		"\n\n" + lipgloss.NewStyle().Foreground(muted).Width(m.contentWidth()).Align(lipgloss.Center).Render("esc to cancel")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) viewSuccess() string {
	w := m.contentWidth()
	body := m.renderLogo(w) + "\n" +
		m.renderMascot(w, "nice one!") + "\n\n" +
		lipgloss.NewStyle().Foreground(green).Bold(true).Width(w).Align(lipgloss.Center).Render("your xeet is out there  ✦")
	if m.postID != "" {
		body += "\n\n" + lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render("https://x.com/i/status/"+m.postID)
	}
	if m.toast != "" {
		body += "\n\n" + lipgloss.NewStyle().Foreground(red).Width(w).Align(lipgloss.Center).Render(m.toast)
	}
	body += "\n\n" + lipgloss.NewStyle().Foreground(muted).Width(w).Align(lipgloss.Center).Render("enter for another  •  q bye")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) viewDialog() string {
	w := m.contentWidth()
	if w > 58 {
		w = 58
	}
	var body string
	switch m.dialog {
	case dialogPath:
		body = lipgloss.NewStyle().Foreground(pink).Bold(true).Render("add a pic  ✦") +
			"\n\nPaste or drag it right here\n" + m.pathInput.View() +
			"\n\n" + lipgloss.NewStyle().Foreground(muted).Render("enter add  •  esc never mind")
	case dialogHelp:
		body = lipgloss.NewStyle().Foreground(pink).Bold(true).Render("little cheat sheet  ✦") +
			"\n\nenter       send your xeet\nalt+enter   new line\nctrl+v      paste clipboard\nctrl+o      add a pic\ntab         pick a pic\ndelete      remove a pic\nesc         head out" +
			"\n\n" + lipgloss.NewStyle().Foreground(muted).Render("f1 or esc to close")
	case dialogQuit:
		body = lipgloss.NewStyle().Foreground(pink).Bold(true).Render("save this draft for later?") +
			"\n\nYour words and pics can be restored next time." +
			"\n\n" + lipgloss.NewStyle().Foreground(muted).Render("enter/y save & leave  •  d discard  •  esc/n stay")
	}
	box := dialogStyle.Width(w - 6).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
