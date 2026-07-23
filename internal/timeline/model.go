package timeline

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"xeet/internal/clip"
	"xeet/pkg/api"
	"xeet/pkg/config"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	blue     = lipgloss.Color("#7AA2F7")
	lavender = lipgloss.Color("#BB9AF7")
	pink     = lipgloss.Color("#F5C2E7")
	muted    = lipgloss.Color("#7D8590")
	red      = lipgloss.Color("#FF757F")
)

type ActionKind int

const (
	ActionQuit ActionKind = iota
	ActionCompose
)

type Action struct{ Kind ActionKind }

type mode int

const (
	modeFeed mode = iota
	modeReply
)

type Model struct {
	width, height int
	posts         []api.TimelinePost
	cursor        string
	selected      int
	starts        []int
	ends          []int
	loading       bool
	loadingMore   bool
	refreshing    bool
	help          bool
	action        Action
	clipboardOK   bool
	toast         string
	err           error
	liking        map[string]bool
	spinner       spinner.Model
	viewport      viewport.Model

	mode         mode
	replyEditor  textarea.Model
	replyPost    api.TimelinePost
	replyPosting bool
	replyErr     error
}

type pageMsg struct {
	page *api.TimelinePage
	err  error
	more bool
}

type actionMsg struct {
	message string
	err     error
}

type likeMsg struct {
	id    string
	liked bool
	err   error
}

type replyResultMsg struct {
	id  string
	err error
}

func New() Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	vp := viewport.New(72, 18)
	editor := textarea.New()
	editor.Prompt = ""
	editor.Placeholder = "write your reply…"
	editor.CharLimit = 280
	editor.ShowLineNumbers = false
	editor.SetWidth(60)
	editor.SetHeight(6)
	return Model{
		width: 80, height: 24, loading: true, liking: map[string]bool{},
		spinner: s, viewport: vp, replyEditor: editor,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, fetchPage("", false))
}

func Run() (Action, error) {
	m := New()
	m.clipboardOK = clip.Init() == nil
	result, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return Action{}, err
	}
	return result.(Model).action, nil
}

func fetchPage(cursor string, more bool) tea.Cmd {
	return func() tea.Msg {
		mgr, err := config.NewConfigManager()
		if err != nil {
			return pageMsg{err: err, more: more}
		}
		cfg, err := mgr.Load()
		if err != nil {
			return pageMsg{err: err, more: more}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		page, err := api.NewWebClient(cfg).FetchHomeTimeline(ctx, cursor, 30)
		return pageMsg{page: page, err: err, more: more}
	}
}

func setLike(tweetID string, liked bool) tea.Cmd {
	return func() tea.Msg {
		mgr, err := config.NewConfigManager()
		if err != nil {
			return likeMsg{id: tweetID, liked: liked, err: err}
		}
		cfg, err := mgr.Load()
		if err != nil {
			return likeMsg{id: tweetID, liked: liked, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = api.NewWebClient(cfg).SetTweetLiked(ctx, tweetID, liked)
		return likeMsg{id: tweetID, liked: liked, err: err}
	}
}

func sendReply(tweetID, text string) tea.Cmd {
	return func() tea.Msg {
		mgr, err := config.NewConfigManager()
		if err != nil {
			return replyResultMsg{err: err}
		}
		cfg, err := mgr.Load()
		if err != nil {
			return replyResultMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		client := api.NewWebClient(cfg)
		id, err := client.PostTweet(ctx, text, tweetID, nil, nil)
		if err == nil && client.Refreshed() {
			cfg.CreateTweetQID = client.QueryID()
			_ = mgr.Save(cfg)
		}
		return replyResultMsg{id: id, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.resize()
		return m, nil
	}

	if m.help {
		if key, ok := msg.(tea.KeyMsg); ok && (key.String() == "?" || key.String() == "esc" || key.String() == "enter") {
			m.help = false
		}
		return m, nil
	}
	if m.mode == modeReply {
		return m.updateReply(msg)
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.loading || m.loadingMore || m.refreshing {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case pageMsg:
		m.loading = false
		m.loadingMore = false
		m.refreshing = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		selectedID := ""
		if post, ok := m.currentPost(); ok {
			selectedID = post.ID
		}
		if !msg.more {
			m.posts = nil
		}
		seen := make(map[string]bool, len(m.posts))
		for _, post := range m.posts {
			seen[post.ID] = true
		}
		for _, post := range msg.page.Posts {
			if !seen[post.ID] {
				m.posts = append(m.posts, post)
				seen[post.ID] = true
			}
		}
		m.cursor = msg.page.Cursor
		m.selected = indexOfPost(m.posts, selectedID)
		if m.selected < 0 {
			m.selected = 0
		}
		m.toast = ""
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, nil
	case actionMsg:
		if msg.err != nil {
			m.toast = msg.err.Error()
		} else {
			m.toast = msg.message
		}
		return m, nil
	case likeMsg:
		delete(m.liking, msg.id)
		if msg.err != nil {
			m.applyLike(msg.id, !msg.liked)
			m.toast = "couldn't update like"
		} else if msg.liked {
			m.toast = "liked ♥"
		} else {
			m.toast = "like removed"
		}
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "?", "f1":
		m.help = true
	case "j", "down":
		m.toast = ""
		if m.selected < len(m.posts)-1 {
			m.selected++
			m.syncViewport()
			m.ensureSelectedVisible()
		}
		if len(m.posts) > 0 && m.selected >= len(m.posts)-5 && m.cursor != "" && !m.loadingMore {
			m.loadingMore = true
			return m, tea.Batch(m.spinner.Tick, fetchPage(m.cursor, true))
		}
	case "k", "up":
		m.toast = ""
		if m.selected > 0 {
			m.selected--
			m.syncViewport()
			m.ensureSelectedVisible()
		}
	case "g", "home":
		m.selected = 0
		m.syncViewport()
		m.ensureSelectedVisible()
	case "G", "end":
		if len(m.posts) > 0 {
			m.selected = len(m.posts) - 1
			m.syncViewport()
			m.ensureSelectedVisible()
		}
	case "R", "ctrl+r":
		if len(m.posts) == 0 {
			m.loading = true
		} else {
			m.refreshing = true
		}
		m.err = nil
		return m, tea.Batch(m.spinner.Tick, fetchPage("", false))
	case "r":
		if post, ok := m.currentPost(); ok {
			m.mode = modeReply
			m.replyPost = post
			m.replyErr = nil
			m.replyEditor.Reset()
			m.resize()
			return m, m.replyEditor.Focus()
		}
	case "P", "c":
		m.action = Action{Kind: ActionCompose}
		return m, tea.Quit
	case "o", "enter":
		if post, ok := m.currentPost(); ok {
			return m, openURL(postURL(post))
		}
	case "l":
		if post, ok := m.currentPost(); ok && !m.liking[post.ID] {
			liked := !post.Liked
			m.liking[post.ID] = true
			m.applyLike(post.ID, liked)
			m.syncViewport()
			m.ensureSelectedVisible()
			return m, setLike(post.ID, liked)
		}
	case "y":
		if post, ok := m.currentPost(); ok {
			if !m.clipboardOK {
				m.toast = "clipboard unavailable"
				return m, nil
			}
			if err := clip.WriteText(postURL(post)); err != nil {
				m.toast = "clipboard unavailable; open the post with Enter"
				return m, nil
			}
			m.toast = "link copied"
		}
	}
	return m, nil
}

func (m Model) updateReply(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.replyPosting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case replyResultMsg:
		m.replyPosting = false
		if msg.err != nil {
			m.replyErr = msg.err
			return m, m.replyEditor.Focus()
		}
		m.mode = modeFeed
		m.replyEditor.Blur()
		m.replyEditor.Reset()
		m.toast = "reply sent ♥"
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if ok {
		if m.replyPosting {
			return m, nil
		}
		switch key.String() {
		case "esc", "ctrl+c":
			m.mode = modeFeed
			m.replyEditor.Blur()
			m.replyErr = nil
			m.syncViewport()
			return m, nil
		case "enter":
			if strings.TrimSpace(m.replyEditor.Value()) == "" {
				m.replyErr = fmt.Errorf("write a reply first")
				return m, nil
			}
			m.replyPosting = true
			m.replyErr = nil
			m.replyEditor.Blur()
			return m, tea.Batch(m.spinner.Tick, sendReply(m.replyPost.ID, m.replyEditor.Value()))
		case "alt+enter", "ctrl+j":
			m.replyEditor.InsertString("\n")
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.replyEditor, cmd = m.replyEditor.Update(msg)
	return m, cmd
}

func (m *Model) resize() {
	w := m.contentWidth()
	viewportHeight := m.height - 4
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	m.viewport.Width = w
	m.viewport.Height = viewportHeight
	m.replyEditor.SetWidth(max(20, w-6))
	m.replyEditor.SetHeight(min(7, max(3, m.height-16)))
	m.syncViewport()
	m.ensureSelectedVisible()
}

func (m *Model) syncViewport() {
	content, starts, ends := m.renderFeedContent()
	m.starts = starts
	m.ends = ends
	m.viewport.SetContent(content)
}

func (m *Model) ensureSelectedVisible() {
	if m.selected < 0 || m.selected >= len(m.starts) {
		return
	}
	start := m.starts[m.selected]
	end := m.ends[m.selected]
	if start < m.viewport.YOffset {
		m.viewport.YOffset = start
	} else if end >= m.viewport.YOffset+m.viewport.Height {
		m.viewport.YOffset = max(0, end-m.viewport.Height+1)
	}
}

func (m Model) currentPost() (api.TimelinePost, bool) {
	if m.selected < 0 || m.selected >= len(m.posts) {
		return api.TimelinePost{}, false
	}
	return m.posts[m.selected], true
}

func (m *Model) applyLike(id string, liked bool) {
	for i := range m.posts {
		if m.posts[i].ID != id || m.posts[i].Liked == liked {
			continue
		}
		m.posts[i].Liked = liked
		if liked {
			m.posts[i].LikeCount++
		} else if m.posts[i].LikeCount > 0 {
			m.posts[i].LikeCount--
		}
		return
	}
}

func indexOfPost(posts []api.TimelinePost, id string) int {
	if id == "" {
		return 0
	}
	for i := range posts {
		if posts[i].ID == id {
			return i
		}
	}
	return -1
}

func postURL(post api.TimelinePost) string {
	handle := post.Handle
	if handle == "" {
		handle = "i"
	}
	return fmt.Sprintf("https://x.com/%s/status/%s", handle, post.ID)
}

func openURL(target string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", target)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
		default:
			cmd = exec.Command("xdg-open", target)
		}
		if err := cmd.Start(); err != nil {
			return actionMsg{err: fmt.Errorf("open post: %w", err)}
		}
		return actionMsg{message: "opened in browser"}
	}
}

func relativeTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	d := time.Since(value)
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d < 7*24*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return value.Format("Jan 2")
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
