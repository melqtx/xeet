package timeline

import (
	"fmt"
	"strconv"
	"strings"

	"xeet/pkg/api"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) View() string {
	if m.width <= 0 {
		return "loading…"
	}
	if m.help {
		return m.clearNativeImages() + m.viewHelp()
	}
	if m.zoom {
		return m.clearNativeImages() + m.viewZoom()
	}
	if m.mode == modeReply {
		return m.clearNativeImages() + m.viewReply()
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
	return m.clearNativeImages() + lipgloss.NewStyle().MarginLeft(left).Render(body)
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
	face := "( o.o )"
	if m.err != nil {
		face = "( >.< )"
	}
	cat := lipgloss.NewStyle().Foreground(pink).Render(" /\\_/\\") +
		lipgloss.NewStyle().Foreground(blue).Bold(true).Render("   xeet") + "\n" +
		lipgloss.NewStyle().Foreground(pink).Render(face) +
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
	if m.contentWidth() < 50 || len(m.posts) == 0 {
		return fmt.Sprintf("%d/%d  ·  ? help", position, len(m.posts))
	}
	if m.expanded {
		return fmt.Sprintf("%d/%d · enter collapse · o browser · ? help", position, len(m.posts))
	}
	return fmt.Sprintf("%d/%d · enter read · l like · r reply · ? help", position, len(m.posts))
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
		block := m.renderPost(post, i == m.selected, abs(i-m.selected) <= inlineImageRadius)
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

func (m Model) renderPost(post api.TimelinePost, selected, nearSelection bool) string {
	width := m.contentWidth()
	name := post.AuthorName
	if name == "" {
		name = "someone"
	}
	when := relativeTime(post.CreatedAt)

	nameColor := dim
	textColor := dim
	handleColor := muted
	gutter := "  "
	if selected {
		nameColor = bright
		textColor = bright
		handleColor = blue
		gutter = lipgloss.NewStyle().Foreground(blue).Render("▎") + " "
	}
	header := gutter +
		lipgloss.NewStyle().Foreground(nameColor).Bold(true).Render(name) + "  " +
		lipgloss.NewStyle().Foreground(handleColor).Render("@"+post.Handle)
	if when != "" {
		header += lipgloss.NewStyle().Foreground(muted).Render(" · " + when)
	}
	header = ansi.Truncate(header, width, "…")

	parts := []string{header}
	indent := gutter + "  "
	body := cleanText(post.Text)
	if len(post.Media) > 0 {
		body = stripTrailingMediaLink(body)
	}
	if body != "" {
		wrapped := lipgloss.NewStyle().Width(max(12, width-4)).Render(highlightEntities(body, textColor))
		textLines := strings.Split(wrapped, "\n")
		if !(selected && m.expanded) && len(textLines) > 4 {
			textLines = textLines[:4]
			textLines[3] = ansi.Truncate(textLines[3], max(2, width-6), "…")
		}
		for _, line := range textLines {
			parts = append(parts, indent+line)
		}
	}

	preview, hasPreview := m.previews[post.ID]
	imageShown := false
	if len(post.Media) > 0 && nearSelection && hasPreview {
		imageBlock := ""
		switch {
		case preview.nativePath != "":
			imageBlock = m.nativePreviewBlock(preview)
		case preview.nativeData != "":
			imageBlock = m.wezTermPreviewBlock(preview)
		case preview.content != "":
			imageBlock = preview.content
		case preview.loading && selected:
			parts = append(parts, indent+lipgloss.NewStyle().Foreground(muted).Render("loading image…"))
			imageShown = true
		}
		if imageBlock != "" {
			parts = append(parts, indent, prefixLines(imageBlock, indent))
			if len(post.Media) > 1 {
				caption := fmt.Sprintf("▣ 1/%d", len(post.Media))
				if selected {
					caption += " · i zoom"
				}
				parts = append(parts, indent+lipgloss.NewStyle().Foreground(muted).Render(caption))
			}
			parts = append(parts, indent)
			imageShown = true
		}
	}
	if len(post.Media) > 0 && !imageShown {
		chip := mediaChip(post)
		if selected && preview.err != nil {
			chip += " · unavailable"
		}
		parts = append(parts, indent+lipgloss.NewStyle().Foreground(muted).Render(chip))
	}

	parts = append(parts, indent+m.actionLine(post))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// prefixLines carries the post's gutter down every row of a multi-line
// block so the selection bar stays unbroken alongside image previews.
func prefixLines(block, prefix string) string {
	lines := strings.Split(block, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (m Model) actionLine(post api.TimelinePost) string {
	quiet := lipgloss.NewStyle().Foreground(muted)
	liked := lipgloss.NewStyle().Foreground(pink)

	like := quiet.Render("♡")
	if post.Liked {
		like = liked.Render("♥")
	}
	if post.LikeCount > 0 {
		count := " " + formatCount(post.LikeCount)
		if post.Liked {
			like += liked.Render(count)
		} else {
			like += quiet.Render(count)
		}
	}
	segments := []string{like}
	if post.ReplyCount > 0 {
		segments = append(segments, quiet.Render("↩ "+formatCount(post.ReplyCount)))
	}
	if post.RepostCount > 0 {
		segments = append(segments, quiet.Render("⟳ "+formatCount(post.RepostCount)))
	}
	if views, err := strconv.Atoi(post.ViewCount); err == nil && views > 0 {
		segments = append(segments, quiet.Render(formatCount(views)+" views"))
	}
	return strings.Join(segments, quiet.Render(" · "))
}

func mediaChip(post api.TimelinePost) string {
	switch post.Media[0].Type {
	case "video":
		return "▶ video"
	case "animated_gif":
		return "▶ gif"
	}
	if len(post.Media) == 1 {
		return "▣ image"
	}
	return fmt.Sprintf("▣ %d images", len(post.Media))
}

func highlightEntities(text string, base lipgloss.Color) string {
	baseStyle := lipgloss.NewStyle().Foreground(base)
	mention := lipgloss.NewStyle().Foreground(blue)
	hashtag := lipgloss.NewStyle().Foreground(lavender)
	link := lipgloss.NewStyle().Foreground(blue).Underline(true)
	words := strings.Fields(text)
	for i, word := range words {
		switch {
		case len(word) > 1 && word[0] == '@':
			words[i] = mention.Render(word)
		case len(word) > 1 && word[0] == '#':
			words[i] = hashtag.Render(word)
		case strings.HasPrefix(word, "https://") || strings.HasPrefix(word, "http://"):
			trimmed := strings.TrimPrefix(strings.TrimPrefix(word, "https://"), "http://")
			words[i] = link.Render(trimmed)
		default:
			words[i] = baseStyle.Render(word)
		}
	}
	return strings.Join(words, " ")
}

// stripTrailingMediaLink removes the t.co link X appends to a post's text
// when its media is attached; the timeline renders the image itself instead.
func stripTrailingMediaLink(text string) string {
	if index := strings.LastIndex(text, " https://t.co/"); index >= 0 && !strings.Contains(text[index+1:], " ") {
		return text[:index]
	}
	if strings.HasPrefix(text, "https://t.co/") && !strings.Contains(text, " ") {
		return ""
	}
	return text
}

func formatCount(value int) string {
	switch {
	case value < 1000:
		return strconv.Itoa(value)
	case value < 1_000_000:
		return compactCount(value, 1000, "k")
	default:
		return compactCount(value, 1_000_000, "m")
	}
}

func compactCount(value, unit int, suffix string) string {
	whole := value / unit
	tenth := (value % unit) * 10 / unit
	if whole >= 10 || tenth == 0 {
		return fmt.Sprintf("%d%s", whole, suffix)
	}
	return fmt.Sprintf("%d.%d%s", whole, tenth, suffix)
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
	length := m.replyEditor.Length()
	counterColor := muted
	switch {
	case length >= 280:
		counterColor = red
	case length >= 260:
		counterColor = yellow
	}
	status := lipgloss.NewStyle().Foreground(counterColor).Render(fmt.Sprintf("%d/280", length)) +
		lipgloss.NewStyle().Foreground(muted).Render("    enter reply  ·  alt+enter newline  ·  esc cancel")
	if m.replyPosting {
		status = lipgloss.NewStyle().Foreground(muted).Render(m.spinner.View() + " sending reply…")
	} else if m.replyErr != nil {
		status = lipgloss.NewStyle().Foreground(red).Render(m.replyErr.Error())
	}
	content := lipgloss.NewStyle().Foreground(pink).Bold(true).Render(title) + "\n" +
		strings.Join(originalLines, "\n") + "\n\n" + editor + "\n\n" + status
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) viewZoom() string {
	post, ok := m.currentPost()
	if !ok || len(post.Media) == 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(muted).Render("nothing to zoom"))
	}
	preview := m.previews[zoomKey(post.ID)]

	name := post.AuthorName
	if name == "" {
		name = "someone"
	}
	title := lipgloss.NewStyle().Foreground(bright).Bold(true).Render(name) +
		lipgloss.NewStyle().Foreground(blue).Render("  @"+post.Handle)
	title = ansi.Truncate(title, max(20, m.width-4), "…")

	var body string
	switch {
	case preview.nativePath != "":
		body = m.nativePreviewBlock(preview)
	case preview.nativeData != "":
		body = m.wezTermPreviewBlock(preview)
	case preview.content != "":
		body = preview.content
	case preview.loading:
		body = lipgloss.NewStyle().Foreground(lavender).Render(m.spinner.View() + " loading image…")
	case preview.err != nil:
		body = lipgloss.NewStyle().Foreground(red).Render("image unavailable")
	}

	parts := []string{title, "", body}
	if alt := strings.TrimSpace(post.Media[0].AltText); alt != "" {
		parts = append(parts, "", lipgloss.NewStyle().Foreground(muted).
			Render(truncateRunes(alt, max(20, m.width-8))))
	}
	parts = append(parts, "", lipgloss.NewStyle().Foreground(muted).Render("i or esc close"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center, parts...))
}

func (m Model) viewHelp() string {
	w := m.contentWidth()
	if w > 54 {
		w = 54
	}
	keys := "\n\n↑ / k       previous\n↓ / j       next\nctrl+d/u    jump five\nl           like / unlike\nr           reply\nR           refresh\nenter       read full post\ni           zoom image\no           open in browser\ny           copy link\nP           new post\ng / G       top / bottom\nctrl+l      redraw screen\nq           quit"
	if m.height < 25 {
		keys = "\n\nj/k move · g/G ends\nl like · r reply · y copy\nenter read · i zoom\nR refresh · o browser\nP compose · ctrl+l redraw\nq quit"
	}
	images := "images: " + string(m.imageMode)
	if m.imageNote != "" && m.height >= 28 {
		images += "\n" + m.imageNote
	}
	body := lipgloss.NewStyle().Foreground(pink).Bold(true).Render("timeline keys") + keys +
		"\n\n" + lipgloss.NewStyle().Foreground(muted).Width(max(20, w-12)).Render(images) +
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
