package timeline

import (
	"fmt"
	"strings"

	"xeet/pkg/api"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if m.width <= 0 {
		return "loading…"
	}
	if m.help {
		return m.viewHelp()
	}
	if m.mode == modeReply {
		return m.viewReply()
	}

	footer := m.footer()
	if m.loading && len(m.posts) == 0 {
		center := lipgloss.Place(m.viewport.Width, m.viewport.Height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(lavender).Render(m.spinner.View()+" gathering xeets…"))
		return m.shell(center, footer)
	}
	if m.err != nil && len(m.posts) == 0 {
		center := lipgloss.Place(m.viewport.Width, m.viewport.Height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(red).Width(max(20, m.viewport.Width-8)).Align(lipgloss.Center).
				Render("the cat lost the timeline\n\n"+m.err.Error()))
		return m.shell(center, "R retry  ·  q quit")
	}
	return m.shell(m.viewport.View(), footer)
}

func (m Model) shell(center, footer string) string {
	w := m.contentWidth()
	body := m.header(w) + "\n\n" + center + "\n" +
		lipgloss.NewStyle().Foreground(muted).Width(w).Align(lipgloss.Center).Render(footer)
	// Keep two columns clear on the right. Filling the terminal's final column
	// triggers autowrap in multiplexers such as Zellij and corrupts frame diffs.
	left := max(0, (m.width-w)/2)
	return lipgloss.NewStyle().MarginLeft(left).Render(body)
}

func (m Model) contentWidth() int {
	w := m.width - 4
	if w > 76 {
		w = 76
	}
	if w < 30 {
		w = 30
	}
	return w
}

func (m Model) header(width int) string {
	status := "home"
	if m.refreshing {
		status = m.spinner.View() + " refreshing"
	} else if m.loadingMore {
		status = m.spinner.View() + " loading more"
	}
	cat := lipgloss.NewStyle().Foreground(pink).Render(" /\\_/\\") +
		lipgloss.NewStyle().Foreground(blue).Bold(true).Render("   xeet") + "\n" +
		lipgloss.NewStyle().Foreground(pink).Render("( o.o )") +
		lipgloss.NewStyle().Foreground(muted).Render("   "+status)
	return lipgloss.NewStyle().Width(width).Render(cat)
}

func (m Model) footer() string {
	if m.toast != "" {
		return m.toast
	}
	if m.err != nil {
		return "refresh failed  ·  R retry"
	}
	position := 0
	if len(m.posts) > 0 {
		position = m.selected + 1
	}
	return fmt.Sprintf("%d/%d  ·  ? help", position, len(m.posts))
}

func (m Model) renderFeedContent() (string, []int, []int) {
	if len(m.posts) == 0 {
		return lipgloss.NewStyle().Foreground(muted).Width(m.contentWidth()).Align(lipgloss.Center).Render("the timeline is quiet"), nil, nil
	}
	blocks := make([]string, 0, len(m.posts))
	starts := make([]int, 0, len(m.posts))
	ends := make([]int, 0, len(m.posts))
	line := 0
	for i, post := range m.posts {
		block := m.renderPost(post, i == m.selected)
		height := lipgloss.Height(block)
		starts = append(starts, line)
		ends = append(ends, line+height-1)
		blocks = append(blocks, block)
		line += height
		if i < len(m.posts)-1 {
			line++
		}
	}
	return strings.Join(blocks, "\n\n"), starts, ends
}

func (m Model) renderPost(post api.TimelinePost, selected bool) string {
	width := m.contentWidth()
	name := post.AuthorName
	if name == "" {
		name = "someone"
	}
	handle := "@" + post.Handle
	when := relativeTime(post.CreatedAt)
	header := name + "  " + handle
	if when != "" {
		header += "  ·  " + when
	}
	header = truncateRunes(header, width-3)

	marker := "  "
	headerColor := muted
	textColor := lipgloss.Color("#A9B1D6")
	if selected {
		marker = "› "
		headerColor = blue
		textColor = lipgloss.Color("#C0CAF5")
	}
	headerLine := lipgloss.NewStyle().Foreground(headerColor).Bold(selected).Render(marker + header)

	wrapped := lipgloss.NewStyle().Width(max(12, width-4)).Render(cleanText(post.Text))
	textLines := strings.Split(wrapped, "\n")
	if len(textLines) > 4 {
		textLines = textLines[:4]
		textLines[3] = truncateRunes(textLines[3], max(2, width-6)) + "…"
	}
	text := lipgloss.NewStyle().Foreground(textColor).PaddingLeft(2).Render(strings.Join(textLines, "\n"))

	like := "Like"
	actionColor := muted
	if post.Liked {
		like = "Liked"
		if selected {
			actionColor = pink
		}
	}
	if m.liking[post.ID] {
		like = "Saving…"
	}
	action := fmt.Sprintf("  %s %d  ·  Reply %d", like, post.LikeCount, post.ReplyCount)
	action = lipgloss.NewStyle().Foreground(actionColor).Render(action)
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, text, action)
}

func (m Model) viewReply() string {
	w := m.contentWidth()
	title := "replying to @" + m.replyPost.Handle
	original := lipgloss.NewStyle().Foreground(muted).Width(max(20, w-8)).Render(cleanText(m.replyPost.Text))
	originalLines := strings.Split(original, "\n")
	if len(originalLines) > 2 {
		originalLines = originalLines[:2]
		originalLines[1] = truncateRunes(originalLines[1], max(2, w-10)) + "…"
	}

	border := blue
	if m.replyErr != nil {
		border = red
	}
	editor := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(0, 1).Width(w - 4).Render(m.replyEditor.View())
	status := fmt.Sprintf("%d/280    enter reply  ·  alt+enter newline  ·  esc cancel", m.replyEditor.Length())
	if m.replyPosting {
		status = m.spinner.View() + " sending reply…"
	} else if m.replyErr != nil {
		status = m.replyErr.Error()
	}
	content := lipgloss.NewStyle().Foreground(pink).Bold(true).Render(title) + "\n" +
		strings.Join(originalLines, "\n") + "\n\n" + editor + "\n\n" +
		lipgloss.NewStyle().Foreground(muted).Render(status)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) viewHelp() string {
	w := m.contentWidth()
	if w > 54 {
		w = 54
	}
	body := lipgloss.NewStyle().Foreground(pink).Bold(true).Render("timeline keys") +
		"\n\n↑ / k       previous\n↓ / j       next\nl           like / unlike\nr           reply\nR           refresh\nenter       open in browser\ny           copy link\nP           new post\ng / G       top / bottom\nq           quit" +
		"\n\n" + lipgloss.NewStyle().Foreground(muted).Render("? or esc close")
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lavender).
		Padding(1, 2).Width(w - 6).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func truncateRunes(value string, maxLen int) string {
	runes := []rune(value)
	if len(runes) <= maxLen {
		return value
	}
	if maxLen <= 1 {
		return "…"
	}
	return string(runes[:maxLen-1]) + "…"
}
