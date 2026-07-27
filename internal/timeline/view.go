package timeline

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/melqtx/xeet/pkg/api"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) View() string {
	if m.width <= 0 {
		return "loading…"
	}
	if m.help {
		return m.viewHelp()
	}
	if m.altText {
		return m.viewAltText()
	}
	if m.zoom {
		return m.viewZoom()
	}
	if m.mode == modeSearch {
		return m.viewSearch()
	}
	if m.mode == modeReply {
		return m.viewReply()
	}
	if m.mode == modeThread {
		return m.viewThread()
	}

	footer := m.footer()
	if m.loading && len(m.posts) == 0 {
		message := m.spinner.View() + " gathering xeets…"
		loadingFooter := footer
		if m.feed == FeedSearch {
			query := ansi.Truncate(m.searchQuery, max(8, m.viewport.Width-20), "…")
			message = m.spinner.View() + " searching “" + query + "”…"
			loadingFooter = "/ edit search  ·  q quit"
		}
		center := lipgloss.Place(m.viewport.Width, m.viewport.Height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(lavender).Render(message))
		return m.shell(center, loadingFooter)
	}
	if m.err != nil && len(m.posts) == 0 {
		title := "the cat lost the timeline"
		errorFooter := m.errorFooter(true)
		if m.feed == FeedSearch {
			title = "search couldn't reach X"
			errorFooter = m.searchErrorFooter(true)
		}
		center := lipgloss.Place(m.viewport.Width, m.viewport.Height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(red).Width(max(20, m.viewport.Width-8)).Align(lipgloss.Center).
				Render(title+"\n\n"+m.err.Error()))
		return m.shell(center, errorFooter)
	}
	if m.feed == FeedSearch && len(m.posts) == 0 {
		query := ansi.Truncate(m.searchQuery, max(8, m.viewport.Width-24), "…")
		message := lipgloss.NewStyle().Foreground(bright).Bold(true).Render("no posts found") + "\n\n" +
			lipgloss.NewStyle().Foreground(muted).Width(max(20, m.viewport.Width-8)).Align(lipgloss.Center).
				Render("nothing matched “"+query+"”\ntry fewer words or remove a search operator")
		center := lipgloss.Place(m.viewport.Width, m.viewport.Height, lipgloss.Center, lipgloss.Center, message)
		return m.shell(center, m.searchEmptyFooter())
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
	status := "for you"
	switch m.feed {
	case FeedForYou:
		status = "for you"
	case FeedFollowing:
		status = "following"
	case FeedBookmarks:
		status = "bookmarks"
	case FeedSearch:
		status = ansi.Truncate("search · “"+m.searchQuery+"”", max(9, width-12), "…")
	}
	if m.mode == modeThread {
		status = "replies"
		if root, ok := m.threadRootPost(); ok && root.Handle != "" {
			status = truncateRunes("replies to @"+root.Handle, max(9, width-12))
		}
	}
	if m.mode == modeThread && (m.threadLoading || m.threadMore) {
		status = m.spinner.View() + " loading replies"
	} else if m.refreshing {
		status = m.spinner.View() + " refreshing"
	} else if m.loadingMore {
		status = m.spinner.View() + " loading more"
	}
	face := "( o.o )"
	if m.err != nil || (m.mode == modeThread && m.threadErr != nil) {
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
		if m.feed == FeedSearch {
			return m.searchErrorFooter(false)
		}
		return m.errorFooter(false)
	}
	position := 0
	if len(m.posts) > 0 {
		position = m.selected + 1
	}
	if m.feed == FeedSearch {
		var footer string
		if m.expanded {
			footer = fmt.Sprintf("%d/%d · / edit · e collapse · o browser · ? help", position, len(m.posts))
		} else {
			footer = fmt.Sprintf("%d/%d · / edit search · R refresh · enter replies · ? help", position, len(m.posts))
		}
		if ansi.StringWidth(footer) > m.contentWidth() {
			return fmt.Sprintf("%d/%d · / edit · ? help", position, len(m.posts))
		}
		return footer
	}
	if m.contentWidth() < 50 || len(m.posts) == 0 {
		return fmt.Sprintf("%d/%d  ·  ? help", position, len(m.posts))
	}
	if m.expanded {
		return fmt.Sprintf("%d/%d · e collapse · o browser · ? help", position, len(m.posts))
	}
	return fmt.Sprintf("%d/%d · enter replies · e read · r reply · ? help", position, len(m.posts))
}

func (m Model) errorFooter(includeQuit bool) string {
	parts := []string{"R retry"}
	if errors.Is(m.err, api.ErrSessionExpired) {
		parts = append([]string{"a reconnect"}, parts...)
	}
	if includeQuit {
		parts = append(parts, "q quit")
	}
	return strings.Join(parts, "  ·  ")
}

func (m Model) searchErrorFooter(includeQuit bool) string {
	edit := "/ edit search"
	if m.contentWidth() < 44 {
		edit = "/ edit"
	}
	parts := []string{"R retry", edit}
	if errors.Is(m.err, api.ErrSessionExpired) {
		parts = append([]string{"a reconnect"}, parts...)
	}
	if includeQuit {
		parts = append(parts, "q quit")
	}
	return strings.Join(parts, "  ·  ")
}

func (m Model) searchEmptyFooter() string {
	footer := "/ edit search  ·  f for you  ·  q quit"
	if ansi.StringWidth(footer) > m.contentWidth() {
		return "/ edit  ·  f for you  ·  q quit"
	}
	return footer
}

func (m Model) viewThread() string {
	footer := m.threadFooter()
	if m.threadLoading && len(m.threadPosts) <= 1 {
		center := lipgloss.Place(m.viewport.Width, m.viewport.Height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(lavender).Render(m.spinner.View()+" gathering replies…"))
		return m.shell(center, footer)
	}
	// The thread opens pre-seeded with the focal post, so a fetch failure
	// almost never leaves the list empty. Show the error over the posts we
	// have instead of hiding it behind a bare retry footer.
	if m.threadErr != nil {
		center := lipgloss.Place(m.viewport.Width, m.viewport.Height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(red).Width(max(20, m.viewport.Width-8)).Align(lipgloss.Center).
				Render("the cat lost the replies\n\n"+m.threadErr.Error()))
		return m.shell(center, footer)
	}
	return m.shell(m.viewport.View(), footer)
}

func (m Model) threadFooter() string {
	if m.toast != "" {
		return m.toast
	}
	if m.threadErr != nil {
		parts := []string{"R retry"}
		if errors.Is(m.threadErr, api.ErrSessionExpired) {
			parts = append([]string{"a reconnect"}, parts...)
		}
		parts = append(parts, "esc back")
		return strings.Join(parts, "  ·  ")
	}
	position := 0
	if len(m.threadPosts) > 0 {
		position = m.selected + 1
	}
	return fmt.Sprintf("%d/%d · r reply · e read · esc back · ? help", position, len(m.threadPosts))
}

func (m Model) renderThreadContent() (string, []int, []int) {
	if len(m.threadPosts) == 0 {
		return lipgloss.NewStyle().Foreground(muted).Width(m.contentWidth()).Align(lipgloss.Center).Render("no replies yet · press r to start one"), nil, nil
	}
	handles := make(map[string]string, len(m.threadPosts))
	for _, item := range m.threadPosts {
		handles[item.ID] = item.Handle
	}
	pieces := make([]string, 0, 2*len(m.threadPosts))
	starts := make([]int, 0, len(m.threadPosts))
	ends := make([]int, 0, len(m.threadPosts))
	line := 0
	for i, item := range m.threadPosts {
		// The gap between posts carries this post's ancestor rails so the
		// conversation reads as one unbroken line down the left.
		if i > 0 {
			pieces = append(pieces, threadSpacer(item.Depth))
			line++
		}
		block := m.renderPost(item.TimelinePost, i == m.selected, true, item.Depth)
		if context := m.replyContext(i, handles); context != "" {
			block = context + "\n" + block
		}
		height := lipgloss.Height(block)
		starts = append(starts, line)
		ends = append(ends, line+height-1)
		pieces = append(pieces, block)
		line += height
	}
	return strings.Join(pieces, "\n"), starts, ends
}

const (
	// maxRailDepth caps how far replies step to the right. A long chain keeps
	// rendering at the deepest rail rather than walking off the frame.
	maxRailDepth = 3
	// feedDepth marks a post that belongs to no conversation. A thread's focal
	// post is depth zero and still carries a rail, since its replies hang from
	// it; a feed post carries none.
	feedDepth = -1
)

func railLevels(depth int) int { return min(max(0, depth), maxRailDepth) }

// railBar is one two-column rail level: the line a post's replies hang from.
func railBar() string { return lipgloss.NewStyle().Foreground(muted).Render("│") + " " }

// threadRail is the prefix every line of a post carries: one rail level for
// each ancestor, then the post's own marker -- the selection bar when it is
// selected, otherwise the rail its own replies will hang from.
func threadRail(depth int, selected bool) string {
	own := railBar()
	if depth == feedDepth {
		own = "  "
	}
	if selected {
		own = lipgloss.NewStyle().Foreground(selectionAccent(depth)).Render("▎") + " "
	}
	return strings.Repeat(railBar(), railLevels(depth)) + own
}

// threadSpacer fills the gap above a post with its ancestors' rails, so the
// conversation's line survives the blank row between two posts.
func threadSpacer(depth int) string {
	levels := railLevels(depth)
	if levels == 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat(railBar(), levels), " ")
}

// selectionAccent separates a reply's selection bar from the focal post's, so
// the color says whether the cursor sits on the conversation's root or in it.
func selectionAccent(depth int) lipgloss.Color {
	if depth > 0 {
		return lavender
	}
	return blue
}

// replyContext names the parent of a reply the rail cannot place: one whose
// parent is not the post directly above it, or one nested past maxRailDepth,
// where every further level draws at the same indent.
func (m Model) replyContext(index int, handles map[string]string) string {
	item := m.threadPosts[index]
	if index == 0 || item.Depth <= 1 {
		return ""
	}
	if item.InReplyToID == m.threadPosts[index-1].ID && item.Depth <= maxRailDepth {
		return ""
	}
	handle := handles[item.InReplyToID]
	if handle == "" {
		return ""
	}
	return threadRail(item.Depth, false) +
		lipgloss.NewStyle().Foreground(muted).Render("↳ @"+handle)
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
		showImage := abs(i-m.selected) <= inlineImageRadius
		if m.imageMode == imageModeNative || m.imageMode == imageModeWezTerm {
			// Native previews are cheap escape sequences rather than large ANSI
			// mosaics. Keep cached images in the feed so they do not disappear as
			// soon as the selection moves away from their post.
			showImage = true
		}
		block := m.renderPost(post, i == m.selected, showImage, feedDepth)
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

// renderPost draws one post. depth is its place in a conversation: feedDepth in
// the timeline, zero for a thread's focal post, and deeper for each reply level,
// which shifts the whole block right behind its rail.
func (m Model) renderPost(post api.TimelinePost, selected, nearSelection bool, depth int) string {
	width := m.contentWidth()
	name := post.AuthorName
	if name == "" {
		name = "someone"
	}
	when := relativeTime(post.CreatedAt)

	nameColor := dim
	textColor := dim
	handleColor := muted
	gutter := threadRail(depth, selected)
	if selected {
		nameColor = bright
		textColor = bright
		handleColor = selectionAccent(depth)
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
	// Text wraps inside whatever the rail leaves it, so a deep reply stays in
	// the same frame as the post it answers.
	pad := lipgloss.Width(indent)
	body := cleanText(post.Text)
	if len(post.Media) > 0 {
		body = stripTrailingMediaLink(body)
	}
	if body != "" {
		wrapped := lipgloss.NewStyle().Width(max(12, width-pad)).Render(highlightEntities(body, textColor))
		textLines := strings.Split(wrapped, "\n")
		if !(selected && m.expanded) && len(textLines) > 4 {
			textLines = textLines[:4]
			textLines[3] = ansi.Truncate(textLines[3], max(2, width-pad-2), "…")
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
			prefix := imagePrefix(indent, gutter, width, previewColumns(preview))
			parts = append(parts, indent, prefixLines(imageBlock, prefix))
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

// imagePrefix keeps a preview inside the frame. Previews are cached per post at
// the width they were fetched, so one loaded in the feed is too wide for the
// same post indented in a thread; the prefix gives up columns rather than let
// the image spill past the right edge and corrupt the frame diff.
func imagePrefix(indent, gutter string, width, columns int) string {
	for _, candidate := range []string{indent, gutter} {
		if columns+lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return ""
}

// previewColumns is the width a preview occupies: native protocols report their
// own cell size, while an ANSI mosaic is measured from its rendered rows.
func previewColumns(preview previewState) int {
	if preview.columns > 0 {
		return preview.columns
	}
	return lipgloss.Width(preview.content)
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
		message := m.replyErr.Error()
		var ambiguous *api.AmbiguousPostError
		var automated *api.AutomationBlockedError
		if errors.As(m.replyErr, &ambiguous) {
			message = "X did not confirm this reply; check your profile, then press b"
		} else if errors.As(m.replyErr, &automated) {
			message = "X rejected this reply as suspected automation; press b to try it in X"
		} else if canOpenReplyInX(m.replyErr) {
			message = "X rejected this reply; add more text or press b to try in X"
		}
		status = lipgloss.NewStyle().Foreground(red).Render(message)
	} else if m.replyNotice != "" {
		status = lipgloss.NewStyle().Foreground(muted).Render(m.replyNotice)
	}
	content := lipgloss.NewStyle().Foreground(pink).Bold(true).Render(title) + "\n" +
		strings.Join(originalLines, "\n") + "\n\n" + editor + "\n\n" + status
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) viewSearch() string {
	w := m.contentWidth()
	title := lipgloss.NewStyle().Foreground(pink).Bold(true).Render("search posts")
	description := "find words, accounts, or exact phrases"
	if m.width < 48 {
		description = "type a query"
	}
	// Keep the text input itself narrower than its styled border so long
	// queries scroll horizontally instead of making the panel grow vertically.
	m.searchInput.Width = max(10, w-16)
	m.searchInput.SetCursor(m.searchInput.Position())
	input := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(blue).
		Padding(0, 1).Width(max(16, w-12)).Render(m.searchInput.View())

	back := m.searchBackLabel()
	hint := "enter search  ·  esc " + back
	if m.width < 48 {
		hint = "enter search  ·  esc " + m.searchBackShortLabel()
	} else if m.width >= 52 {
		hint += "  ·  ctrl+u clear"
	}
	content := title + "\n" +
		lipgloss.NewStyle().Foreground(muted).Render(description) + "\n\n" +
		input + "\n\n" +
		lipgloss.NewStyle().Foreground(muted).Render(hint)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lavender).
		Padding(1, 2).Width(max(20, w-6)).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) searchBackLabel() string {
	if m.searchReturn == modeThread {
		return "back to replies"
	}
	switch m.feed {
	case FeedFollowing:
		return "back to following"
	case FeedBookmarks:
		return "back to bookmarks"
	case FeedSearch:
		if m.searchQuery == "" && len(m.posts) == 0 {
			return "quit"
		}
		return "back to results"
	default:
		return "back to for you"
	}
}

func (m Model) searchBackShortLabel() string {
	if m.searchReturn == modeThread {
		return "replies"
	}
	switch m.feed {
	case FeedFollowing:
		return "following"
	case FeedBookmarks:
		return "bookmarks"
	case FeedSearch:
		if m.searchQuery == "" && len(m.posts) == 0 {
			return "quit"
		}
		return "results"
	default:
		return "for you"
	}
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

func (m Model) altTextPanelWidth() int {
	return min(64, max(28, m.width-8))
}

func (m Model) altTextVisibleRows() int {
	// Border, padding, title, account, spacer, and footer consume eight rows.
	return max(1, m.height-8)
}

func (m Model) altTextRows() []string {
	post, ok := m.currentPost()
	if !ok {
		return nil
	}
	width := m.altTextPanelWidth() - 6
	var rows []string
	for index, item := range post.Media {
		description := strings.TrimSpace(item.AltText)
		if description == "" {
			description = "No alt text was provided."
		}
		label := fmt.Sprintf("image %d of %d", index+1, len(post.Media))
		rows = append(rows, lipgloss.NewStyle().Foreground(blue).Bold(true).Render(label))
		wrapped := lipgloss.NewStyle().Foreground(bright).Width(width).Render(description)
		rows = append(rows, strings.Split(wrapped, "\n")...)
		if index < len(post.Media)-1 {
			rows = append(rows, "")
		}
	}
	return rows
}

func (m Model) altTextMaxScroll() int {
	return max(0, len(m.altTextRows())-m.altTextVisibleRows())
}

func (m Model) viewAltText() string {
	post, ok := m.currentPost()
	if !ok || len(post.Media) == 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(muted).Render("this post has no images"))
	}
	w := m.altTextPanelWidth()
	name := post.AuthorName
	if name == "" {
		name = "someone"
	}
	rows := m.altTextRows()
	start := max(0, min(m.altTextMaxScroll(), m.altTextScroll))
	end := min(len(rows), start+m.altTextVisibleRows())
	visible := rows[start:end]

	footer := "A/enter/esc close"
	if len(rows) > m.altTextVisibleRows() {
		footer = fmt.Sprintf("↑/↓ scroll  ·  %d-%d/%d  ·  %s", start+1, end, len(rows), footer)
	}
	content := []string{
		lipgloss.NewStyle().Foreground(pink).Bold(true).Render("image descriptions"),
		lipgloss.NewStyle().Foreground(muted).Render(name + "  @" + post.Handle), "",
	}
	content = append(content, visible...)
	content = append(content, lipgloss.NewStyle().Foreground(muted).Render(footer))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lavender).
		Padding(1, 2).Width(w).Render(lipgloss.JoinVertical(lipgloss.Left, content...))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) viewHelp() string {
	w := m.contentWidth()
	if w > 54 {
		w = 54
	}
	keys := "\n\n↑ / k       previous\n↓ / j       next\nctrl+d/u    jump five\nf           for you / following\nb           bookmarks / for you\n/           search\nl           like / unlike\nr           reply\nR           refresh\nenter       open replies\ne / space   read full post\ni           zoom image\nA           image alt text\no           open in browser\ny           copy link\nP           new post\ng / G       top / bottom\nctrl+l      redraw screen\nq           quit"
	if m.mode == modeThread {
		keys = "\n\n↑ / k       previous\n↓ / j       next\nctrl+d/u    jump five\n/           search\nl           like / unlike\nr           reply to selected\nR           refresh replies\ne / space   read full post\ni           zoom image\nA           image alt text\no           open in browser\ny           copy link\ng / G       top / bottom\nctrl+l      redraw screen\nesc         back to timeline\nq           quit"
	}
	if m.height < 25 {
		keys = "\n\nj/k move · g/G ends · f feed\nl like · r reply · y copy\nenter replies · e read · i zoom · A alt text\nR refresh · o browser\nP new · / search · ^L redraw\nb bookmarks · q quit"
		if m.mode == modeThread {
			keys = "\n\nj/k move · g/G ends\nl like · r reply · y copy\ne read · i zoom · A alt text\nR refresh · o browser · / search\nesc back · q quit"
		}
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
