package timeline

import (
	"context"
	"fmt"
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
	yellow   = lipgloss.Color("#E0AF68")
	bright   = lipgloss.Color("#C0CAF5")
	dim      = lipgloss.Color("#A9B1D6")
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
	imageMode     imageMode
	imageNote     string
	posts         []api.TimelinePost
	cursor        string
	selected      int
	starts        []int
	ends          []int
	loading       bool
	loadingMore   bool
	refreshing    bool
	help          bool
	expanded      bool
	zoom          bool
	action        Action
	clipboardOK   bool
	toast         string
	toastSeq      int
	err           error
	liking        map[string]bool
	previews      map[string]previewState
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

type toastClearMsg struct{ seq int }

type clockTickMsg time.Time

func clockTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg { return clockTickMsg(t) })
}

func (m *Model) showToast(text string) tea.Cmd {
	m.toast = text
	m.toastSeq++
	seq := m.toastSeq
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return toastClearMsg{seq} })
}

type previewState struct {
	content    string
	nativePath string
	nativeData string
	imageID    uint32
	columns    int
	rows       int
	loading    bool
	err        error
}

type previewMsg struct {
	postID     string
	content    string
	nativePath string
	nativeData string
	imageID    uint32
	columns    int
	rows       int
	err        error
}

func New() Model {
	return NewWithImageMode("auto")
}

func NewWithImageMode(requested string) Model {
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
	mode, note := resolveImageMode(requested)
	return Model{
		width: 80, height: 24, imageMode: mode, imageNote: note, loading: true,
		liking: map[string]bool{}, previews: map[string]previewState{},
		spinner: s, viewport: vp, replyEditor: editor,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, fetchPage("", false), clockTick())
}

func Run(images string) (Action, error) {
	m := NewWithImageMode(images)
	// Auto-detected native mode is a claim, not a capability: multiplexers
	// like cmux inherit ghostty's TERM without reliably rendering graphics.
	// Confirm with the terminal itself; --images native skips the probe.
	if m.imageMode == imageModeNative && images != "native" {
		if err := probeNativeGraphics(); err != nil {
			m.imageMode = imageModeANSI
			m.imageNote = "terminal didn't confirm kitty graphics — using ansi (--images native to force)"
		}
	}
	m.clipboardOK = clip.Init() == nil
	result, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return Action{}, err
	}
	final := result.(Model)
	final.cleanupPreviews()
	return final.action, nil
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
		return m, m.imageRepaint()
	}
	if _, ok := msg.(clockTickMsg); ok {
		if m.mode == modeFeed && !m.help {
			m.syncViewport()
		}
		return m, clockTick()
	}
	if clear, ok := msg.(toastClearMsg); ok {
		if clear.seq == m.toastSeq {
			m.toast = ""
		}
		return m, nil
	}

	if m.help {
		if key, ok := msg.(tea.KeyMsg); ok && (key.String() == "?" || key.String() == "esc" || key.String() == "enter") {
			m.help = false
			return m, m.imageRepaint()
		}
		return m, nil
	}
	if m.zoom {
		// Keys close the zoom view; everything else (preview arrivals,
		// spinner ticks) flows through to the main handler below.
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "i", "esc", "q", "enter":
				m.zoom = false
				return m, m.imageRepaint()
			}
			return m, nil
		}
	}
	if m.mode == modeReply {
		return m.updateReply(msg)
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.loading || m.loadingMore || m.refreshing || m.zoomLoading() {
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
		seen := make(map[string]bool, len(m.posts))
		for _, post := range m.posts {
			seen[post.ID] = true
		}
		var toast tea.Cmd
		if !msg.more && len(m.posts) > 0 {
			// In-place refresh: unseen posts stack on top, the selection stays
			// on the same post, and the old cursor keeps pagination intact.
			var fresh []api.TimelinePost
			for _, post := range msg.page.Posts {
				if !seen[post.ID] {
					fresh = append(fresh, post)
				}
			}
			if len(fresh) > 0 {
				m.posts = append(fresh, m.posts...)
				m.selected += len(fresh)
				label := "posts"
				if len(fresh) == 1 {
					label = "post"
				}
				toast = m.showToast(fmt.Sprintf("%d new %s · g jumps to top", len(fresh), label))
			} else {
				toast = m.showToast("all caught up")
			}
		} else {
			selectedID := ""
			if post, ok := m.currentPost(); ok {
				selectedID = post.ID
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
		}
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, m.imageRepaint(m.requestPreviews(), toast)
	case actionMsg:
		if msg.err != nil {
			return m, m.showToast(msg.err.Error())
		}
		return m, m.showToast(msg.message)
	case previewMsg:
		m.previews[msg.postID] = previewState{
			content: msg.content, nativePath: msg.nativePath, nativeData: msg.nativeData, imageID: msg.imageID,
			columns: msg.columns, rows: msg.rows, err: msg.err,
		}
		m.evictDistantPreviews()
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, m.imageRepaint()
	case likeMsg:
		delete(m.liking, msg.id)
		var toast tea.Cmd
		if msg.err != nil {
			m.applyLike(msg.id, !msg.liked)
			toast = m.showToast("couldn't update like")
		} else if msg.liked {
			toast = m.showToast("liked ♥")
		} else {
			toast = m.showToast("like removed")
		}
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, toast
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.expanded {
			m.expanded = false
			m.syncViewport()
			m.ensureSelectedVisible()
			return m, m.imageRepaint()
		}
		return m, tea.Quit
	case "?", "f1":
		m.help = true
		return m, m.imageRepaint()
	case "j", "down":
		m.moveSelection(m.selected + 1)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMore())
	case "k", "up":
		m.moveSelection(m.selected - 1)
		return m, m.imageRepaint(m.requestPreviews())
	case "ctrl+d":
		m.moveSelection(m.selected + 5)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMore())
	case "ctrl+u":
		m.moveSelection(m.selected - 5)
		return m, m.imageRepaint(m.requestPreviews())
	case "g", "home":
		m.moveSelection(0)
		return m, m.imageRepaint(m.requestPreviews())
	case "G", "end":
		m.moveSelection(len(m.posts) - 1)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMore())
	case "ctrl+l":
		m.syncViewport()
		return m, func() tea.Msg { return tea.ClearScreen() }
	case "R", "ctrl+r":
		if len(m.posts) == 0 {
			m.loading = true
		} else {
			m.refreshing = true
		}
		m.err = nil
		return m, m.imageRepaint(tea.Batch(m.spinner.Tick, fetchPage("", false)))
	case "r":
		if post, ok := m.currentPost(); ok {
			m.mode = modeReply
			m.replyPost = post
			m.replyErr = nil
			m.replyEditor.Reset()
			m.resize()
			return m, m.imageRepaint(m.replyEditor.Focus())
		}
	case "P", "c":
		m.action = Action{Kind: ActionCompose}
		return m, tea.Quit
	case "enter":
		if len(m.posts) > 0 {
			m.expanded = !m.expanded
			m.syncViewport()
			m.ensureSelectedVisible()
			return m, m.imageRepaint()
		}
	case "o":
		if post, ok := m.currentPost(); ok {
			return m, openURL(postURL(post))
		}
	case "i":
		if post, ok := m.currentPost(); ok && len(post.Media) > 0 {
			if m.imageMode == imageModeOff {
				return m, m.showToast("image previews are off (--images)")
			}
			m.zoom = true
			if zoom := m.requestZoom(); zoom != nil {
				return m, m.imageRepaint(tea.Batch(m.spinner.Tick, zoom))
			}
			return m, m.imageRepaint()
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
				return m, m.showToast("clipboard unavailable")
			}
			if err := clip.WriteText(postURL(post)); err != nil {
				return m, m.showToast("clipboard unavailable; open the post with o")
			}
			return m, m.showToast("link copied")
		}
	}
	return m, nil
}

func (m *Model) moveSelection(target int) {
	if len(m.posts) == 0 {
		return
	}
	target = max(0, min(len(m.posts)-1, target))
	if target != m.selected {
		m.selected = target
		m.expanded = false
	}
	m.toast = ""
	m.syncViewport()
	m.ensureSelectedVisible()
}

func (m *Model) maybeLoadMore() tea.Cmd {
	if len(m.posts) > 0 && m.selected >= len(m.posts)-5 && m.cursor != "" && !m.loadingMore {
		m.loadingMore = true
		return tea.Batch(m.spinner.Tick, fetchPage(m.cursor, true))
	}
	return nil
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
		toast := m.showToast("reply sent ♥")
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, m.imageRepaint(toast)
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
			return m, m.imageRepaint()
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

func (m Model) imageRepaint(cmds ...tea.Cmd) tea.Cmd {
	filtered := make([]tea.Cmd, 0, len(cmds)+1)
	for _, cmd := range cmds {
		if cmd != nil {
			filtered = append(filtered, cmd)
		}
	}
	if m.imageMode == imageModeWezTerm {
		filtered = append(filtered, func() tea.Msg { return tea.ClearScreen() })
	}
	if len(filtered) == 0 {
		return nil
	}
	return tea.Batch(filtered...)
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
	// Keep a small margin above and below the selection so movement never
	// pins it to the viewport edge (vim's scrolloff).
	const margin = 2
	start := m.starts[m.selected]
	end := m.ends[m.selected]
	top := m.viewport.YOffset
	if start-margin < top {
		top = max(0, start-margin)
	} else if end+margin >= top+m.viewport.Height {
		top = end + margin - m.viewport.Height + 1
	}
	// A post taller than the viewport anchors to its own first line.
	if start < top {
		top = start
	}
	maxTop := max(0, m.ends[len(m.ends)-1]+1-m.viewport.Height)
	m.viewport.YOffset = max(0, min(top, maxTop))
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
		if err := openExternalURL(target); err != nil {
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
