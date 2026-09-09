package timeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"
)

type profileState struct {
	info    api.Profile
	posts   []api.TimelinePost
	cursor  string
	loading bool
	err     error
}
type profileBack struct {
	profileSelected, profileOffset int
	mode                           mode
	selected, offset               int
	expanded                       bool
	profile                        profileState
	thread                         *threadState
}
type profileMsg struct {
	seq  int
	more bool
	info *api.Profile
	page *api.TimelinePage
	err  error
}

func (m *Model) requestProfile(more bool) tea.Cmd {
	m.profileSeq++
	seq, info, cursor, parent := m.profileSeq, m.profile.info, m.profile.cursor, m.requestContext()
	m.profile.loading, m.profile.err = true, nil
	return func() tea.Msg {
		msg := profileMsg{seq: seq, more: more}
		mgr, err := config.NewConfigManager()
		if err != nil {
			msg.err = err
			return msg
		}
		cfg, err := mgr.Load()
		if err != nil {
			msg.err = err
			return msg
		}
		ctx, cancel := context.WithTimeout(parent, 40*time.Second)
		defer cancel()
		client := api.NewWebClient(cfg)
		defer func() {
			if client.ApplyRefreshedQueryIDs(cfg) {
				_ = mgr.Save(cfg)
			}
		}()
		if !more {
			msg.info, err = client.FetchProfile(ctx, info.Handle)
			if err != nil {
				msg.err = err
				return msg
			}
			info = *msg.info
			cursor = ""
		}
		msg.page, msg.err = client.FetchProfilePosts(ctx, info.ID, cursor, 30)
		return msg
	}
}

func (m Model) beginProfile(post api.TimelinePost) (tea.Model, tea.Cmd) {
	if post.Handle == "" {
		return m, m.showToast("this post has no author profile")
	}
	back := profileBack{mode: m.mode, selected: m.selected, offset: m.viewport.YOffset, expanded: m.expanded, profile: m.profile, thread: m.snapshotThread(), profileSelected: m.profileSelected, profileOffset: m.profileOffset}
	back.profile.loading = false
	m.profileStack = append(m.profileStack, back)
	if m.mode == modeFeed {
		m.feedSelected = m.selected
	}
	m.mode = modeProfile
	m.profile = profileState{info: api.Profile{Account: api.Account{Name: post.AuthorName, Handle: post.Handle}}}
	m.selected, m.viewport.YOffset, m.expanded = 0, 0, false
	m.toast = ""
	cmd := m.requestProfile(false)
	m.syncViewport()
	return m, m.imageRepaint(tea.Batch(m.spinner.Tick, cmd))
}

func (m Model) leaveProfile() (tea.Model, tea.Cmd) {
	if len(m.profileStack) == 0 {
		return m, nil
	}
	back := m.profileStack[len(m.profileStack)-1]
	m.profileStack = m.profileStack[:len(m.profileStack)-1]
	m.profileSeq++
	m.mode, m.selected, m.expanded = back.mode, back.selected, back.expanded
	m.profile = back.profile
	m.profileSelected, m.profileOffset = back.profileSelected, back.profileOffset
	m.restoreThread(back.thread)
	if back.mode == modeFeed {
		m.selected = m.feedSelected
	}
	m.syncViewport()
	m.viewport.SetYOffset(back.offset)
	return m, m.imageRepaint(m.requestPreviews())
}

func (m Model) applyProfilePage(msg profileMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.profileSeq {
		return m, nil
	}
	m.profile.loading, m.profile.err = false, msg.err
	if msg.info != nil {
		m.profile.info = *msg.info
	}
	if msg.err == nil && msg.page != nil {
		selectedID := ""
		if m.selected >= 0 && m.selected < len(m.profile.posts) {
			selectedID = m.profile.posts[m.selected].ID
		}
		posts := []api.TimelinePost{}
		seen := map[string]bool{}
		if msg.more {
			posts = append(posts, m.profile.posts...)
			for _, p := range posts {
				seen[p.ID] = true
			}
		}
		for _, p := range msg.page.Posts {
			if !seen[p.ID] {
				posts = append(posts, p)
				seen[p.ID] = true
			}
		}
		m.profile.posts, m.profile.cursor = posts, msg.page.Cursor
		if m.mode == modeProfile {
			m.selected = 0
			for i, p := range posts {
				if p.ID == selectedID {
					m.selected = i
					break
				}
			}
		}
	}
	if m.mode != modeProfile {
		return m, nil
	}
	m.syncViewport()
	if msg.more {
		m.ensureSelectedVisible()
	}
	return m, m.imageRepaint(m.requestPreviews())
}

func (m Model) updateProfile(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.profile.loading || m.zoomLoading() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case pageMsg:
		return m.applyFeedPage(msg)
	case likeMsg:
		return m, m.applyLikeResult(msg)
	case previewMsg:
		return m, m.applyPreview(msg)
	case actionMsg:
		if msg.err != nil {
			return m, m.showToast(msg.err.Error())
		}
		return m, m.showToast(msg.message)
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	target := m.selected
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m.leaveProfile()
	case "?", "f1":
		m.help, m.helpScroll = true, 0
		return m, m.imageRepaint()
	case "j", "down":
		target++
	case "k", "up":
		target--
	case "ctrl+d":
		target += 5
	case "ctrl+u":
		target -= 5
	case "g", "home":
		target = 0
	case "G", "end":
		target = len(m.profile.posts) - 1
	case "pgdown":
		m.viewport.SetYOffset(m.viewport.YOffset + max(1, m.viewport.Height-2))
		return m, m.imageRepaint()
	case "pgup":
		m.viewport.SetYOffset(m.viewport.YOffset - max(1, m.viewport.Height-2))
		return m, m.imageRepaint()
	case "ctrl+l":
		m.syncViewport()
		return m, func() tea.Msg { return tea.ClearScreen() }
	case "R", "ctrl+r":
		if m.profile.loading {
			return m, nil
		}
		cmd := m.requestProfile(false)
		m.syncViewport()
		return m, tea.Batch(m.spinner.Tick, cmd)
	case "enter":
		if post, ok := m.currentPost(); ok {
			m.profileSelected, m.profileOffset = m.selected, m.viewport.YOffset
			return m.beginThread(post, modeProfile)
		}
		return m, nil
	case "r":
		if post, ok := m.currentPost(); ok {
			return m.beginReply(post)
		}
		return m, nil
	case "e", " ":
		m.expanded = !m.expanded
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, m.imageRepaint()
	case "o":
		return m, m.openSelected()
	case "y":
		return m, m.copySelectedLink()
	case "l":
		return m, m.toggleSelectedLike()
	case "i":
		return m, m.zoomSelected()
	case "A":
		return m, m.showAltText()
	case "v":
		return m, m.playSelectedVideo()
	default:
		return m, nil
	}
	if len(m.profile.posts) > 0 {
		m.selected = max(0, min(target, len(m.profile.posts)-1))
	}
	m.expanded, m.toast = false, ""
	m.syncViewport()
	m.ensureSelectedVisible()
	if target == 0 {
		m.viewport.SetYOffset(0)
	}
	var more tea.Cmd
	if len(m.profile.posts) > 0 && m.selected >= len(m.profile.posts)-5 && m.profile.cursor != "" && !m.profile.loading && m.profile.err == nil {
		more = tea.Batch(m.spinner.Tick, m.requestProfile(true))
	}
	return m, m.imageRepaint(m.requestPreviews(), more)
}

func (m Model) renderProfileContent() (string, []int, []int) {
	p, w := m.profile.info, m.contentWidth()
	title := lipgloss.NewStyle().Foreground(blue).Bold(true).Render(">>@" + cleanText(p.Handle))
	title += "  " + lipgloss.NewStyle().Foreground(bright).Bold(true).Render(cleanText(p.Name))
	lines := []string{ansi.Truncate(title, w, "…")}
	if p.Protected {
		lines = append(lines, lipgloss.NewStyle().Foreground(muted).Render("[ protected account ]"))
	}
	if p.Bio != "" {
		for _, line := range strings.Split(ansi.Wrap(cleanText(p.Bio), w-2, ""), "\n") {
			lines = append(lines, lipgloss.NewStyle().Foreground(green).Render("> "+line))
		}
	}
	details := []string{}
	if p.Location != "" {
		details = append(details, cleanText(p.Location))
	}
	if joined, err := time.Parse(time.RubyDate, p.Joined); err == nil {
		details = append(details, "joined "+joined.Format("Jan 2006"))
	}
	if len(details) > 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(muted).Render(ansi.Wrap(strings.Join(details, " · "), w, "")))
	}
	if p.Website != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(blue).Render(ansi.Wrap(cleanText(p.Website), w, "")))
	}
	if p.ID != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(muted).Render(ansi.Wrap(fmt.Sprintf("%s posts · %s followers · %s following", profileCount(p.Posts, p.HasPosts), profileCount(p.Followers, p.HasFollowers), profileCount(p.Following, p.HasFollowing)), w, "")))
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(muted).Render(strings.Repeat("─", w)), "[ posts ]")
	if m.profile.loading {
		lines = append(lines, m.spinner.View()+" loading posts…")
	}
	if m.profile.err != nil {
		lines = append(lines, lipgloss.NewStyle().Foreground(red).Render(ansi.Wrap(cleanText(m.profile.err.Error())+" · R retry", w, "")))
	}
	if !m.profile.loading && m.profile.err == nil && len(m.profile.posts) == 0 {
		lines = append(lines, "no posts to show")
	}
	header := strings.Join(lines, "\n") + "\n\n"
	copy := m
	copy.posts = m.profile.posts
	content, starts, ends := copy.renderFeedContent()
	if len(m.profile.posts) == 0 {
		return strings.TrimRight(header, "\n"), nil, nil
	}
	offset := strings.Count(header, "\n")
	for i := range starts {
		starts[i] += offset
		ends[i] += offset
	}
	return header + content, starts, ends
}

func profileCount(value int, available bool) string {
	if !available {
		return "—"
	}
	return formatCount(value)
}
