package timeline

import (
	"context"
	"errors"
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
	ActionAuthenticate
)

type Action struct{ Kind ActionKind }

type mode int

const (
	modeFeed mode = iota
	modeThread
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
	altText       bool
	altTextScroll int
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

	mode          mode
	feedSelected  int
	threadRootID  string
	threadPosts   []api.ConversationPost
	threadCursor  string
	threadLoading bool
	threadMore    bool
	threadErr     error
	threadSeq     int
	replyReturn   mode
	replyEditor   textarea.Model
	replyPost     api.TimelinePost
	replyPosting  bool
	replyErr      error
	replyNotice   string
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

type threadMsg struct {
	rootID string
	seq    int
	page   *api.ConversationPage
	err    error
	more   bool
}

type replyBrowserMsg struct{ err error }

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
			m.imageNote = "terminal didn't confirm kitty graphics; using ansi (--images native to force)"
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
		client := api.NewWebClient(cfg)
		page, err := client.FetchHomeTimeline(ctx, cursor, 30)
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.Save(cfg)
		}
		return pageMsg{page: page, err: err, more: more}
	}
}

func fetchThread(tweetID, cursor string, more bool, seq int) tea.Cmd {
	return func() tea.Msg {
		mgr, err := config.NewConfigManager()
		if err != nil {
			return threadMsg{rootID: tweetID, seq: seq, err: err, more: more}
		}
		cfg, err := mgr.Load()
		if err != nil {
			return threadMsg{rootID: tweetID, seq: seq, err: err, more: more}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		client := api.NewWebClient(cfg)
		page, err := client.FetchTweetDetail(ctx, tweetID, cursor, 40)
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.Save(cfg)
		}
		return threadMsg{rootID: tweetID, seq: seq, page: page, err: err, more: more}
	}
}

func (m *Model) requestThread(cursor string, more bool) tea.Cmd {
	m.threadSeq++
	return fetchThread(m.threadRootID, cursor, more, m.threadSeq)
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
		client := api.NewWebClient(cfg)
		err = client.SetTweetLiked(ctx, tweetID, liked)
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.Save(cfg)
		}
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
		if client.ApplyRefreshedQueryIDs(cfg) {
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
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "?", "esc", "enter":
				m.help = false
				return m, m.imageRepaint()
			}
			// Help keys must not trigger actions in the feed underneath it.
			return m, nil
		}
		// Keep applying background results while help is open.
	}
	if m.altText {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "A", "esc", "enter":
				m.altText = false
				return m, m.imageRepaint()
			case "j", "down":
				m.moveAltText(1)
			case "k", "up":
				m.moveAltText(-1)
			case "pgdown", "ctrl+d":
				m.moveAltText(m.altTextVisibleRows())
			case "pgup", "ctrl+u":
				m.moveAltText(-m.altTextVisibleRows())
			case "home", "g":
				m.altTextScroll = 0
			case "end", "G":
				m.altTextScroll = m.altTextMaxScroll()
			}
			// Panel keys must not trigger actions in the feed underneath it.
			return m, nil
		}
		// Background results (likes, pages, and previews) must still reach the
		// normal handlers while the panel is open.
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
	if m.mode == modeThread {
		return m.updateThread(msg)
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.loading || m.loadingMore || m.refreshing || m.zoomLoading() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case pageMsg:
		return m.applyFeedPage(msg)
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
			return m.beginReply(post)
		}
	case "a":
		if errors.Is(m.err, api.ErrSessionExpired) {
			m.action = Action{Kind: ActionAuthenticate}
			return m, tea.Quit
		}
	case "P", "c":
		m.action = Action{Kind: ActionCompose}
		return m, tea.Quit
	case "enter":
		if post, ok := m.currentPost(); ok {
			m.feedSelected = m.selected
			m.mode = modeThread
			m.threadRootID = post.ID
			m.threadPosts = []api.ConversationPost{{TimelinePost: post}}
			m.threadCursor = ""
			m.threadLoading = true
			m.threadErr = nil
			m.selected = 0
			m.expanded = false
			m.viewport.YOffset = 0
			m.syncViewport()
			return m, m.imageRepaint(tea.Batch(m.spinner.Tick, m.requestThread("", false), m.requestPreviews()))
		}
	case " ", "e":
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
	case "A":
		if post, ok := m.currentPost(); ok && len(post.Media) > 0 {
			m.altText = true
			m.altTextScroll = 0
			return m, m.imageRepaint()
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

func (m *Model) moveAltText(delta int) {
	m.altTextScroll = max(0, min(m.altTextMaxScroll(), m.altTextScroll+delta))
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

func (m Model) applyFeedPage(msg pageMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	m.loadingMore = false
	m.refreshing = false
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.err = nil
	threadContext := m.mode == modeThread || (m.mode == modeReply && m.replyReturn == modeThread)
	feedIndex := m.selected
	if threadContext {
		feedIndex = m.feedSelected
	}
	selectedID := ""
	if feedIndex >= 0 && feedIndex < len(m.posts) {
		selectedID = m.posts[feedIndex].ID
	}
	seen := make(map[string]bool, len(m.posts))
	for _, post := range m.posts {
		seen[post.ID] = true
	}
	var toast tea.Cmd
	if !msg.more && len(m.posts) > 0 {
		var fresh []api.TimelinePost
		for _, post := range msg.page.Posts {
			if !seen[post.ID] {
				fresh = append(fresh, post)
			}
		}
		if len(fresh) > 0 {
			m.posts = append(fresh, m.posts...)
			feedIndex += len(fresh)
			label := "posts"
			if len(fresh) == 1 {
				label = "post"
			}
			toast = m.showToast(fmt.Sprintf("%d new %s · g jumps to top", len(fresh), label))
		} else {
			toast = m.showToast("all caught up")
		}
	} else {
		for _, post := range msg.page.Posts {
			if !seen[post.ID] {
				m.posts = append(m.posts, post)
				seen[post.ID] = true
			}
		}
		m.cursor = msg.page.Cursor
		feedIndex = indexOfPost(m.posts, selectedID)
		if feedIndex < 0 {
			feedIndex = 0
		}
		m.toast = ""
	}
	if threadContext {
		m.feedSelected = feedIndex
	} else {
		m.selected = feedIndex
	}
	if m.mode == modeReply {
		return m, toast
	}
	m.syncViewport()
	m.ensureSelectedVisible()
	if m.mode == modeFeed {
		return m, m.imageRepaint(m.requestPreviews(), toast)
	}
	return m, toast
}

func (m Model) beginReply(post api.TimelinePost) (tea.Model, tea.Cmd) {
	m.replyReturn = m.mode
	m.mode = modeReply
	m.replyPost = post
	m.replyErr = nil
	m.replyNotice = ""
	m.replyEditor.Reset()
	m.resize()
	return m, m.imageRepaint(m.replyEditor.Focus())
}

func (m Model) applyThreadPage(msg threadMsg, repaint bool) (tea.Model, tea.Cmd) {
	if msg.rootID != m.threadRootID || msg.seq != m.threadSeq {
		return m, nil
	}
	m.threadLoading = false
	m.threadMore = false
	if msg.err != nil {
		m.threadErr = msg.err
		return m, nil
	}
	m.threadErr = nil
	selectedID := ""
	if m.selected >= 0 && m.selected < len(m.threadPosts) {
		selectedID = m.threadPosts[m.selected].ID
	}
	seen := map[string]bool{}
	var merged []api.ConversationPost
	if msg.more {
		merged = append(merged, m.threadPosts...)
		for _, post := range merged {
			seen[post.ID] = true
		}
	}
	for _, post := range m.resolveConversationPosts(msg.page) {
		if !seen[post.ID] {
			merged = append(merged, post)
			seen[post.ID] = true
		}
	}
	// X can omit the focal post from a refresh while still returning replies.
	// The conversation must always keep its root: without it a refresh would
	// silently turn the thread into a list of orphaned replies.
	if !seen[m.threadRootID] {
		if root, ok := m.threadRootPost(); ok {
			merged = append([]api.ConversationPost{root}, merged...)
		}
	}
	m.threadPosts = merged
	m.normalizeThreadDepths()
	m.threadCursor = msg.page.Cursor
	m.selected = indexOfConversationPost(m.threadPosts, selectedID)
	if m.selected < 0 {
		m.selected = 0
	}
	if repaint {
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, m.imageRepaint(m.requestPreviews())
	}
	return m, nil
}

func (m Model) threadRootPost() (api.ConversationPost, bool) {
	for _, post := range m.threadPosts {
		if post.ID == m.threadRootID {
			post.Depth = 0
			return post, true
		}
	}
	return api.ConversationPost{}, false
}

func (m Model) resolveConversationPosts(page *api.ConversationPage) []api.ConversationPost {
	resolved := append([]api.ConversationPost(nil), page.Posts...)
	depths := make(map[string]int, len(m.threadPosts)+len(resolved))
	for _, post := range m.threadPosts {
		depths[post.ID] = post.Depth
	}
	for _, post := range resolved {
		depths[post.ID] = post.Depth
	}
	pending := append([]api.TimelinePost(nil), page.Unresolved...)
	for len(pending) > 0 {
		progress := false
		next := pending[:0]
		for _, post := range pending {
			parentDepth, ok := depths[post.InReplyToID]
			if !ok {
				next = append(next, post)
				continue
			}
			depth := parentDepth + 1
			resolved = append(resolved, api.ConversationPost{TimelinePost: post, Depth: depth})
			depths[post.ID] = depth
			progress = true
		}
		if !progress {
			break
		}
		pending = next
	}
	return resolved
}

func (m *Model) normalizeThreadDepths() {
	byID := make(map[string]api.ConversationPost, len(m.threadPosts))
	for _, post := range m.threadPosts {
		byID[post.ID] = post
	}
	for i := range m.threadPosts {
		if m.threadPosts[i].ID == m.threadRootID {
			m.threadPosts[i].Depth = 0
			continue
		}
		depth := 1
		parent := m.threadPosts[i].InReplyToID
		visited := map[string]bool{m.threadPosts[i].ID: true}
		for parent != "" && parent != m.threadRootID && !visited[parent] {
			visited[parent] = true
			ancestor, ok := byID[parent]
			if !ok {
				break
			}
			parent = ancestor.InReplyToID
			depth++
		}
		m.threadPosts[i].Depth = depth
	}
}

func (m Model) updateThread(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.threadLoading || m.threadMore || m.zoomLoading() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case pageMsg:
		return m.applyFeedPage(msg)
	case threadMsg:
		return m.applyThreadPage(msg, true)
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
	case previewMsg:
		m.previews[msg.postID] = previewState{
			content: msg.content, nativePath: msg.nativePath, nativeData: msg.nativeData, imageID: msg.imageID,
			columns: msg.columns, rows: msg.rows, err: msg.err,
		}
		m.evictDistantPreviews()
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, m.imageRepaint()
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
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeFeed
		m.selected = m.feedSelected
		m.threadSeq++
		m.threadRootID = ""
		m.expanded = false
		m.threadLoading = false
		m.threadMore = false
		// An expired session affects every request, not just this thread.
		// Hand the error to the feed so its reconnect flow stays reachable.
		if errors.Is(m.threadErr, api.ErrSessionExpired) {
			m.err = m.threadErr
		}
		m.threadErr = nil
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, m.imageRepaint()
	case "a":
		if errors.Is(m.threadErr, api.ErrSessionExpired) {
			m.action = Action{Kind: ActionAuthenticate}
			return m, tea.Quit
		}
	case "ctrl+l":
		m.syncViewport()
		return m, func() tea.Msg { return tea.ClearScreen() }
	case "?", "f1":
		m.help = true
		return m, m.imageRepaint()
	case "j", "down":
		m.moveThreadSelection(m.selected + 1)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMoreThread())
	case "k", "up":
		m.moveThreadSelection(m.selected - 1)
		return m, m.imageRepaint(m.requestPreviews())
	case "ctrl+d":
		m.moveThreadSelection(m.selected + 5)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMoreThread())
	case "ctrl+u":
		m.moveThreadSelection(m.selected - 5)
		return m, m.imageRepaint(m.requestPreviews())
	case "g", "home":
		m.moveThreadSelection(0)
		return m, m.imageRepaint(m.requestPreviews())
	case "G", "end":
		m.moveThreadSelection(len(m.threadPosts) - 1)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMoreThread())
	case "R", "ctrl+r":
		m.threadLoading = true
		m.threadMore = false
		m.threadErr = nil
		return m, m.imageRepaint(tea.Batch(m.spinner.Tick, m.requestThread("", false)))
	case "r":
		if post, ok := m.currentPost(); ok {
			return m.beginReply(post)
		}
	case "enter", " ", "e":
		m.expanded = !m.expanded
		m.syncViewport()
		m.ensureSelectedVisible()
		return m, m.imageRepaint()
	case "o":
		if post, ok := m.currentPost(); ok {
			return m, openURL(postURL(post))
		}
	case "A":
		if post, ok := m.currentPost(); ok && len(post.Media) > 0 {
			m.altText = true
			m.altTextScroll = 0
			return m, m.imageRepaint()
		}
	case "i":
		if post, ok := m.currentPost(); ok && len(post.Media) > 0 {
			if m.imageMode == imageModeOff {
				return m, m.showToast("image previews are off (--images)")
			}
			m.zoom = true
			return m, m.imageRepaint(tea.Batch(m.spinner.Tick, m.requestZoom()))
		}
	case "l":
		if post, ok := m.currentPost(); ok && !m.liking[post.ID] {
			liked := !post.Liked
			m.liking[post.ID] = true
			m.applyLike(post.ID, liked)
			m.syncViewport()
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

func (m *Model) moveThreadSelection(target int) {
	if len(m.threadPosts) == 0 {
		return
	}
	m.selected = max(0, min(len(m.threadPosts)-1, target))
	m.expanded = false
	m.toast = ""
	m.syncViewport()
	m.ensureSelectedVisible()
}

func (m *Model) maybeLoadMoreThread() tea.Cmd {
	if len(m.threadPosts) > 0 && m.selected >= len(m.threadPosts)-5 && m.threadCursor != "" && !m.threadLoading && !m.threadMore {
		m.threadMore = true
		return tea.Batch(m.spinner.Tick, m.requestThread(m.threadCursor, true))
	}
	return nil
}

func (m Model) updateReply(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case threadMsg:
		if m.replyReturn != modeThread {
			return m, nil
		}
		return m.applyThreadPage(msg, false)
	case pageMsg:
		return m.applyFeedPage(msg)
	case likeMsg:
		delete(m.liking, msg.id)
		if msg.err != nil {
			m.applyLike(msg.id, !msg.liked)
		}
		return m, nil
	case previewMsg:
		m.previews[msg.postID] = previewState{
			content: msg.content, nativePath: msg.nativePath, nativeData: msg.nativeData, imageID: msg.imageID,
			columns: msg.columns, rows: msg.rows, err: msg.err,
		}
		return m, nil
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
			m.replyNotice = ""
			return m, m.replyEditor.Focus()
		}
		m.mode = m.replyReturn
		m.replyEditor.Blur()
		m.replyEditor.Reset()
		toast := m.showToast("reply sent ♥")
		m.syncViewport()
		m.ensureSelectedVisible()
		if m.mode == modeThread {
			m.threadLoading = true
			m.threadMore = false
			return m, m.imageRepaint(tea.Batch(toast, m.spinner.Tick, m.requestThread("", false)))
		}
		return m, m.imageRepaint(toast)
	case replyBrowserMsg:
		if msg.err != nil {
			m.replyErr = fmt.Errorf("couldn't open X: %w", msg.err)
			m.replyNotice = ""
			return m, nil
		}
		m.replyErr = nil
		m.replyNotice = "opened reply in X"
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if ok {
		if m.replyPosting {
			return m, nil
		}
		switch key.String() {
		case "b":
			if canOpenReplyInX(m.replyErr) {
				return m, openReplyInX(m.replyPost.ID, m.replyEditor.Value())
			}
		case "esc", "ctrl+c":
			m.mode = m.replyReturn
			m.replyEditor.Blur()
			m.replyErr = nil
			m.replyNotice = ""
			m.syncViewport()
			return m, m.imageRepaint(m.requestPreviews())
		case "enter":
			if strings.TrimSpace(m.replyEditor.Value()) == "" {
				m.replyErr = fmt.Errorf("write a reply first")
				return m, nil
			}
			m.replyPosting = true
			m.replyErr = nil
			m.replyNotice = ""
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

func canOpenReplyInX(err error) bool {
	var automated *api.AutomationBlockedError
	if errors.As(err, &automated) {
		return true
	}
	var restricted *api.PostingRestrictedError
	if errors.As(err, &restricted) {
		return true
	}
	var recent *api.RecentlyPostedError
	if errors.As(err, &recent) {
		return true
	}
	var ambiguous *api.AmbiguousPostError
	return errors.As(err, &ambiguous)
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
	var content string
	var starts, ends []int
	if m.mode == modeThread {
		content, starts, ends = m.renderThreadContent()
	} else {
		content, starts, ends = m.renderFeedContent()
	}
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

func (m Model) activePosts() []api.TimelinePost {
	if m.mode == modeThread {
		posts := make([]api.TimelinePost, len(m.threadPosts))
		for i := range m.threadPosts {
			posts[i] = m.threadPosts[i].TimelinePost
		}
		return posts
	}
	return m.posts
}

func (m Model) currentPost() (api.TimelinePost, bool) {
	if m.mode == modeThread {
		if m.selected < 0 || m.selected >= len(m.threadPosts) {
			return api.TimelinePost{}, false
		}
		return m.threadPosts[m.selected].TimelinePost, true
	}
	if m.selected < 0 || m.selected >= len(m.posts) {
		return api.TimelinePost{}, false
	}
	return m.posts[m.selected], true
}

func (m *Model) applyLike(id string, liked bool) {
	apply := func(post *api.TimelinePost) {
		if post.ID != id || post.Liked == liked {
			return
		}
		post.Liked = liked
		if liked {
			post.LikeCount++
		} else if post.LikeCount > 0 {
			post.LikeCount--
		}
	}
	for i := range m.posts {
		apply(&m.posts[i])
	}
	for i := range m.threadPosts {
		apply(&m.threadPosts[i].TimelinePost)
	}
}

func indexOfConversationPost(posts []api.ConversationPost, id string) int {
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
