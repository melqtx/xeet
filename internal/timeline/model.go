package timeline

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/melqtx/xeet/internal/clip"
	"github.com/melqtx/xeet/internal/theme"
	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	blue     lipgloss.Color
	lavender lipgloss.Color
	pink     lipgloss.Color
	muted    lipgloss.Color
	red      lipgloss.Color
	yellow   lipgloss.Color
	bright   lipgloss.Color
	dim      lipgloss.Color
)

func init() { ApplyTheme(theme.Default()) }

// ApplyTheme recolors the timeline. Call it before Run; layout is unaffected.
func ApplyTheme(p theme.Palette) {
	blue, lavender, pink, muted = p.Blue, p.Lavender, p.Pink, p.Muted
	red, yellow, bright, dim = p.Red, p.Yellow, p.Bright, p.Dim
}

type ActionKind int

const (
	ActionQuit ActionKind = iota
	ActionCompose
	ActionAuthenticate
)

type Action struct{ Kind ActionKind }

// FeedKind identifies which timeline the feed pane is showing.
type FeedKind int

const (
	FeedForYou FeedKind = iota
	FeedFollowing
	FeedBookmarks
	FeedSearch
	FeedList
)

type mode int

const (
	modeFeed mode = iota
	modeThread
	modeReply
	modeSearch
	modeListPicker
)

type Model struct {
	// ctx bounds every request this model starts. It carries the process's
	// interrupt signal, so a SIGINT or SIGTERM cancels in-flight fetches
	// instead of leaving them to run out their own timeouts.
	ctx           context.Context
	width, height int
	imageMode     imageMode
	imageNote     string
	help          bool
	altText       bool
	altTextScroll int
	zoom          bool
	action        Action
	clipboardOK   bool
	toast         string
	toastSeq      int
	liking        map[string]bool
	previews      map[string]previewState
	spinner       spinner.Model
	columns       []column
	focus         int
	nextColID     int

	mode              mode
	replyReturn       mode
	replyEditor       textarea.Model
	replyPost         api.TimelinePost
	replyPosting      bool
	replyErr          error
	replyNotice       string
	searchInput       textinput.Model
	searchReturn      mode
	listPicker        []api.ListInfo
	listPickerSel     int
	listPickerErr     error
	listPickerLoading bool
	listReturn        mode
}

type pageMsg struct {
	page  *api.TimelinePage
	err   error
	more  bool
	seq   int
	colID int
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
	colID  int
	page   *api.ConversationPage
	err    error
	more   bool
}

type replyBrowserMsg struct{ err error }

type toastClearMsg struct{ seq int }

// wezRepaintMsg asks the model to decide whether the iTerm2-protocol frame
// moved and therefore needs a full clear before Bubble Tea repaints.
type wezRepaintMsg struct{}

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
	colID      int
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
	editor := textarea.New()
	editor.Prompt = ""
	editor.Placeholder = "write your reply…"
	editor.CharLimit = 280
	editor.ShowLineNumbers = false
	editor.SetWidth(60)
	editor.SetHeight(6)
	search := textinput.New()
	search.Placeholder = `from:alice "terminal ui"`
	search.CharLimit = 512
	mode, note := resolveImageMode(requested)
	return Model{
		ctx:   context.Background(),
		width: 80, height: 24, imageMode: mode, imageNote: note,
		liking: map[string]bool{}, previews: map[string]previewState{},
		spinner: s, columns: []column{newColumn(0)},
		nextColID: 1, replyEditor: editor, searchInput: search,
	}
}

// requestContext is the parent for every fetch. Models built directly in
// tests may leave ctx nil.
func (m Model) requestContext() context.Context {
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func (m Model) Init() tea.Cmd {
	c := m.cur()
	if c.feed == FeedSearch && c.searchQuery == "" {
		return tea.Batch(m.searchInput.Focus(), clockTick())
	}
	if m.mode == modeListPicker {
		return tea.Batch(m.spinner.Tick, fetchListsCmd(m.requestContext()), clockTick())
	}
	cmds := []tea.Cmd{m.spinner.Tick}
	for i := range m.columns {
		c := &m.columns[i]
		cmds = append(cmds, fetchPageSeq(
			m.requestContext(), c.feed, c.searchQuery, c.listID, "", false, c.feedSeq, c.id,
		))
	}
	cmds = append(cmds, clockTick())
	return tea.Batch(cmds...)
}

func Run(ctx context.Context, images string, feed FeedKind, query, listID string, columnCount int) (Action, error) {
	m := NewWithImageMode(images)
	m.ctx = ctx
	m.configureColumns(columnCount, feed, query, listID)
	if feed == FeedSearch && query == "" {
		m.cur().loading = false
		m.beginSearch()
	}
	if feed == FeedList {
		if listID == "" {
			m.cur().loading = false
			m.beginListPicker()
		}
	}
	// Auto-detected native mode is a claim, not a capability: multiplexers
	// like cmux inherit ghostty's TERM without reliably rendering graphics.
	// Confirm with the terminal itself; --images native skips the probe.
	// Inside tmux the probe reply never reaches this pane, so native mode is
	// vetted by tmuxNativeSupport during resolveImageMode instead.
	if m.imageMode == imageModeNative && images != "native" && !insideTmux() {
		if err := probeNativeGraphics(); err != nil {
			m.imageMode = imageModeANSI
			m.imageNote = "terminal didn't confirm kitty graphics; using ansi (--images native to force)"
		}
	}
	m.clipboardOK = clip.Init() == nil
	result, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	// Native previews leave PNGs in the temp directory, so they get cleaned up
	// on every exit path, including an interrupt.
	if final, ok := result.(Model); ok {
		final.cleanupPreviews()
		if err == nil {
			return final.action, nil
		}
	}
	if errors.Is(err, tea.ErrProgramKilled) {
		// An interrupt reached the program through ctx. That is an ordinary
		// exit, not a failure worth reporting.
		return Action{}, nil
	}
	return Action{}, err
}

func fetchPageSeq(parent context.Context, feed FeedKind, query, listID, cursor string, more bool, seq, colID int) tea.Cmd {
	return func() tea.Msg {
		mgr, err := config.NewConfigManager()
		if err != nil {
			return pageMsg{err: err, more: more, seq: seq, colID: colID}
		}
		cfg, err := mgr.Load()
		if err != nil {
			return pageMsg{err: err, more: more, seq: seq, colID: colID}
		}
		ctx, cancel := context.WithTimeout(parent, 40*time.Second)
		defer cancel()
		client := api.NewWebClient(cfg)
		fetch := client.FetchHomeTimeline
		switch feed {
		case FeedFollowing:
			fetch = client.FetchFollowingTimeline
		case FeedBookmarks:
			fetch = client.FetchBookmarks
		case FeedSearch:
			q := query
			fetch = func(ctx context.Context, cursor string, count int) (*api.TimelinePage, error) {
				return client.FetchSearchTimeline(ctx, q, cursor, count)
			}
		case FeedList:
			id := listID
			fetch = func(ctx context.Context, cursor string, count int) (*api.TimelinePage, error) {
				return client.FetchListTimeline(ctx, id, cursor, count)
			}
		}
		page, err := fetch(ctx, cursor, 30)
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.Save(cfg)
		}
		return pageMsg{page: page, err: err, more: more, seq: seq, colID: colID}
	}
}

func setLike(parent context.Context, tweetID string, liked bool) tea.Cmd {
	return func() tea.Msg {
		mgr, err := config.NewConfigManager()
		if err != nil {
			return likeMsg{id: tweetID, liked: liked, err: err}
		}
		cfg, err := mgr.Load()
		if err != nil {
			return likeMsg{id: tweetID, liked: liked, err: err}
		}
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		defer cancel()
		client := api.NewWebClient(cfg)
		err = client.SetTweetLiked(ctx, tweetID, liked)
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.Save(cfg)
		}
		return likeMsg{id: tweetID, liked: liked, err: err}
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
	if _, ok := msg.(wezRepaintMsg); ok {
		if m.imageMode != imageModeWezTerm {
			return m, nil
		}
		// iTerm2-protocol images are painted pixels, not cells: when content
		// scrolls, Bubble Tea's line diff can leave stale pixels behind on
		// rows whose text did not change. Clearing forces a full repaint, but
		// doing it on every keystroke made iTerm2 flicker constantly, so it
		// only happens when the frame's layout actually moved.
		if key := m.imageFrameKey(); key != m.cur().wezFrameKey {
			m.cur().wezFrameKey = key
			return m, func() tea.Msg { return tea.ClearScreen() }
		}
		return m, nil
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
	switch m.mode {
	case modeReply:
		return m.updateReply(msg)
	case modeSearch:
		return m.updateSearch(msg)
	case modeListPicker:
		return m.updateListPicker(msg)
	case modeThread:
		return m.updateThread(msg)
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.columnsLoading() || m.zoomLoading() {
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
		return m, m.applyPreview(msg)
	case likeMsg:
		return m, m.applyLikeResult(msg)
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "tab", "]":
		m.cycleFocus(1)
		return m, m.imageRepaint()
	case "shift+tab", "[":
		m.cycleFocus(-1)
		return m, m.imageRepaint()
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.cur().expanded {
			m.cur().expanded = false
			m.syncViewport()
			m.ensureSelectedVisible()
			return m, m.imageRepaint()
		}
		return m, tea.Quit
	case "?", "f1":
		m.help = true
		return m, m.imageRepaint()
	case "j", "down":
		m.moveSelection(m.cur().selected + 1)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMore())
	case "k", "up":
		m.moveSelection(m.cur().selected - 1)
		return m, m.imageRepaint(m.requestPreviews())
	case "ctrl+d":
		m.moveSelection(m.cur().selected + 5)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMore())
	case "ctrl+u":
		m.moveSelection(m.cur().selected - 5)
		return m, m.imageRepaint(m.requestPreviews())
	case "g", "home":
		m.moveSelection(0)
		return m, m.imageRepaint(m.requestPreviews())
	case "G", "end":
		m.moveSelection(len(m.cur().posts) - 1)
		return m, m.imageRepaint(m.requestPreviews(), m.maybeLoadMore())
	case "ctrl+l":
		m.syncViewport()
		return m, func() tea.Msg { return tea.ClearScreen() }
	case "f":
		return m, m.switchFeed()
	case "b":
		if m.cur().feed == FeedBookmarks {
			return m, m.setFeed(FeedForYou)
		}
		return m, m.setFeed(FeedBookmarks)
	case "/":
		return m, m.beginSearch()
	case "L":
		return m, m.beginListPicker()
	case "R", "ctrl+r":
		c := m.cur()
		if len(c.posts) == 0 {
			c.loading = true
		} else {
			c.refreshing = true
		}
		c.err = nil
		return m, m.imageRepaint(tea.Batch(m.spinner.Tick, fetchPageSeq(
			m.requestContext(), c.feed, c.searchQuery, c.listID, "", false, c.feedSeq, c.id,
		)))
	case "r":
		if post, ok := m.currentPost(); ok {
			return m.beginReply(post)
		}
	case "a":
		if errors.Is(m.cur().err, api.ErrSessionExpired) {
			m.action = Action{Kind: ActionAuthenticate}
			return m, tea.Quit
		}
	case "P", "c":
		m.action = Action{Kind: ActionCompose}
		return m, tea.Quit
	case "enter":
		if post, ok := m.currentPost(); ok {
			m.cur().feedSelected = m.cur().selected
			m.mode = modeThread
			m.cur().threadRootID = post.ID
			m.cur().threadPosts = []api.ConversationPost{{TimelinePost: post}}
			m.cur().threadCursor = ""
			m.cur().threadLoading = true
			m.cur().threadErr = nil
			m.cur().selected = 0
			m.cur().expanded = false
			m.cur().viewport.YOffset = 0
			m.syncViewport()
			return m, m.imageRepaint(tea.Batch(m.spinner.Tick, m.requestThread("", false), m.requestPreviews()))
		}
	case " ", "e":
		if len(m.cur().posts) > 0 {
			m.cur().expanded = !m.cur().expanded
			m.syncViewport()
			m.ensureSelectedVisible()
			return m, m.imageRepaint()
		}
	case "o":
		return m, m.openSelected()
	case "A":
		return m, m.showAltText()
	case "i":
		return m, m.zoomSelected()
	case "l":
		return m, m.toggleSelectedLike()
	case "y":
		return m, m.copySelectedLink()
	}
	return m, nil
}

// The handlers below back both the feed and a thread. Both lists render the
// same post blocks and react to results the same way, so keeping one copy
// stops the two modes from drifting apart.

// settleLike clears the in-flight marker and rolls an optimistic like back if
// the request failed.
func (m *Model) settleLike(msg likeMsg) {
	delete(m.liking, msg.id)
	if msg.err != nil {
		m.applyLike(msg.id, !msg.liked)
	}
}

func (m *Model) applyLikeResult(msg likeMsg) tea.Cmd {
	m.settleLike(msg)
	toast := "like removed"
	switch {
	case msg.err != nil:
		toast = "couldn't update like"
	case msg.liked:
		toast = "liked ♥"
	}
	cmd := m.showToast(toast)
	m.syncViewport()
	m.ensureSelectedVisible()
	return cmd
}

func (m *Model) storePreview(msg previewMsg) *column {
	c := m.columnByID(msg.colID)
	if c == nil {
		return nil
	}
	m.previews[msg.postID] = previewState{
		content: msg.content, nativePath: msg.nativePath, nativeData: msg.nativeData, imageID: msg.imageID,
		columns: msg.columns, rows: msg.rows, err: msg.err,
	}
	return c
}

func (m *Model) applyPreview(msg previewMsg) tea.Cmd {
	c := m.storePreview(msg)
	if c == nil {
		return nil
	}
	m.evictDistantPreviews()
	if c != m.cur() {
		return nil
	}
	m.syncViewport()
	m.ensureSelectedVisible()
	return m.imageRepaint()
}

func (m *Model) openSelected() tea.Cmd {
	post, ok := m.currentPost()
	if !ok {
		return nil
	}
	return openURL(postURL(post))
}

func (m *Model) showAltText() tea.Cmd {
	if post, ok := m.currentPost(); !ok || len(post.Media) == 0 {
		return nil
	}
	m.altText = true
	m.altTextScroll = 0
	return m.imageRepaint()
}

func (m *Model) zoomSelected() tea.Cmd {
	if post, ok := m.currentPost(); !ok || len(post.Media) == 0 {
		return nil
	}
	if m.imageMode == imageModeOff {
		return m.showToast("image previews are off (--images)")
	}
	m.zoom = true
	if zoom := m.requestZoom(); zoom != nil {
		return m.imageRepaint(tea.Batch(m.spinner.Tick, zoom))
	}
	return m.imageRepaint()
}

func (m *Model) toggleSelectedLike() tea.Cmd {
	post, ok := m.currentPost()
	if !ok || m.liking[post.ID] {
		return nil
	}
	liked := !post.Liked
	m.liking[post.ID] = true
	m.applyLike(post.ID, liked)
	m.syncViewport()
	m.ensureSelectedVisible()
	return setLike(m.requestContext(), post.ID, liked)
}

func (m *Model) copySelectedLink() tea.Cmd {
	post, ok := m.currentPost()
	if !ok {
		return nil
	}
	if !m.clipboardOK {
		return m.showToast("clipboard unavailable")
	}
	if err := clip.WriteText(postURL(post)); err != nil {
		return m.showToast("clipboard unavailable; open the post with o")
	}
	return m.showToast("link copied")
}

func (m *Model) moveAltText(delta int) {
	m.altTextScroll = max(0, min(m.altTextMaxScroll(), m.altTextScroll+delta))
}

func (m *Model) moveSelection(target int) {
	if len(m.cur().posts) == 0 {
		return
	}
	target = max(0, min(len(m.cur().posts)-1, target))
	if target != m.cur().selected {
		m.cur().selected = target
		m.cur().expanded = false
	}
	m.toast = ""
	m.syncViewport()
	m.ensureSelectedVisible()
}

func (m *Model) cycleFocus(delta int) {
	if len(m.columns) <= 1 {
		return
	}
	m.focus = (m.focus + delta + len(m.columns)) % len(m.columns)
	m.resize()
}

func (m Model) columnsLoading() bool {
	for i := range m.columns {
		c := &m.columns[i]
		if c.loading || c.loadingMore || c.refreshing {
			return true
		}
	}
	return false
}

// setFeed resets feed state and kicks off a fresh first page for kind.
func (m *Model) setFeed(kind FeedKind) tea.Cmd {
	c := m.cur()
	c.feed = kind
	c.feedSeq++
	c.posts = nil
	c.cursor = ""
	c.selected = 0
	c.expanded = false
	c.loading = true
	c.loadingMore = false
	c.refreshing = false
	c.err = nil
	c.viewport.YOffset = 0
	m.syncViewport()
	return m.imageRepaint(tea.Batch(m.spinner.Tick, fetchPageSeq(
		m.requestContext(), c.feed, c.searchQuery, c.listID, "", false, c.feedSeq, c.id,
	)))
}

// switchFeed keeps the f-key semantics: toggle For You <-> Following.
// From any other feed kind it returns to For You.
func (m *Model) switchFeed() tea.Cmd {
	if m.cur().feed == FeedForYou {
		return m.setFeed(FeedFollowing)
	}
	return m.setFeed(FeedForYou)
}

func (m *Model) maybeLoadMore() tea.Cmd {
	c := m.cur()
	if len(c.posts) > 0 && c.selected >= len(c.posts)-5 && c.cursor != "" && !c.loadingMore {
		c.loadingMore = true
		return tea.Batch(m.spinner.Tick, fetchPageSeq(
			m.requestContext(), c.feed, c.searchQuery, c.listID, c.cursor, true, c.feedSeq, c.id,
		))
	}
	return nil
}

func (m Model) applyFeedPage(msg pageMsg) (tea.Model, tea.Cmd) {
	c := m.columnByID(msg.colID)
	if c == nil || msg.seq != c.feedSeq {
		// A page from before the last feed switch; the wrong feed's posts
		// must not leak into the current one.
		return m, nil
	}
	c.loading = false
	c.loadingMore = false
	c.refreshing = false
	if msg.err != nil {
		c.err = msg.err
		return m, nil
	}
	c.err = nil
	focused := c == m.cur()
	threadContext := focused && (m.mode == modeThread ||
		(m.mode == modeReply && m.replyReturn == modeThread) ||
		(m.mode == modeSearch && m.searchReturn == modeThread) ||
		(m.mode == modeListPicker && m.listReturn == modeThread))
	feedIndex := c.selected
	if threadContext {
		feedIndex = c.feedSelected
	}
	selectedID := ""
	if feedIndex >= 0 && feedIndex < len(c.posts) {
		selectedID = c.posts[feedIndex].ID
	}
	seen := make(map[string]bool, len(c.posts))
	for _, post := range c.posts {
		seen[post.ID] = true
	}
	var toast tea.Cmd
	if !msg.more && len(c.posts) > 0 {
		var fresh []api.TimelinePost
		for _, post := range msg.page.Posts {
			if !seen[post.ID] {
				fresh = append(fresh, post)
			}
		}
		if len(fresh) > 0 {
			c.posts = append(fresh, c.posts...)
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
				c.posts = append(c.posts, post)
				seen[post.ID] = true
			}
		}
		c.cursor = msg.page.Cursor
		feedIndex = indexOfPost(c.posts, selectedID)
		if feedIndex < 0 {
			feedIndex = 0
		}
		m.toast = ""
	}
	if threadContext {
		c.feedSelected = feedIndex
	} else {
		c.selected = feedIndex
	}
	if !focused {
		first, count := m.visibleColumnRange()
		for index := first; index < first+count; index++ {
			if &m.columns[index] == c {
				m.syncFeedColumn(c, columnContentWidth(m.width, count), false)
				break
			}
		}
	}
	if !focused || m.mode == modeReply {
		return m, toast
	}
	m.syncViewport()
	m.ensureSelectedVisible()
	if m.mode == modeFeed {
		return m, m.imageRepaint(m.requestPreviews(), toast)
	}
	return m, toast
}

func (m Model) imageRepaint(cmds ...tea.Cmd) tea.Cmd {
	filtered := make([]tea.Cmd, 0, len(cmds)+1)
	for _, cmd := range cmds {
		if cmd != nil {
			filtered = append(filtered, cmd)
		}
	}
	if m.imageMode == imageModeWezTerm {
		filtered = append(filtered, func() tea.Msg { return wezRepaintMsg{} })
	}
	if len(filtered) == 0 {
		return nil
	}
	return tea.Batch(filtered...)
}

// imageFrameKey fingerprints everything that determines where images sit on
// screen: scroll offset, viewport geometry, overlay state, and the layout of
// every rendered block. Selection-only changes keep the same key because
// block heights do not move.
func (m Model) imageFrameKey() string {
	hash := fnv.New64a()
	fmt.Fprintf(hash, "%d|%d|%d|%d|%v|%v|%v|%v|%v|",
		m.cur().viewport.YOffset, m.cur().viewport.Width, m.cur().viewport.Height,
		m.mode, m.help, m.altText, m.zoom, m.cur().expanded, m.cur().loading)
	for _, start := range m.cur().starts {
		fmt.Fprintf(hash, "%d,", start)
	}
	if len(m.cur().ends) > 0 {
		fmt.Fprintf(hash, "|%d", m.cur().ends[len(m.cur().ends)-1])
	}
	return fmt.Sprintf("%x", hash.Sum64())
}

func (m *Model) resize() {
	viewportHeight := m.height - 4
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	if m.mode == modeThread {
		m.cur().viewport.Width = columnContentWidth(m.width, 1)
		m.cur().viewport.Height = viewportHeight
		m.syncViewport()
	} else {
		first, count := m.visibleColumnRange()
		width := columnContentWidth(m.width, count)
		for index := first; index < first+count; index++ {
			c := &m.columns[index]
			c.viewport.Width = width
			c.viewport.Height = viewportHeight
			m.syncFeedColumn(c, width, index == m.focus)
		}
	}
	overlayWidth := columnContentWidth(m.width, 1)
	m.replyEditor.SetWidth(max(20, overlayWidth-6))
	m.replyEditor.SetHeight(min(7, max(3, m.height-16)))
	m.searchInput.Width = max(10, overlayWidth-16)
	m.searchInput.SetCursor(m.searchInput.Position())
	m.ensureSelectedVisible()
}

func (m *Model) syncViewport() {
	if m.mode == modeThread {
		m.cur().viewport.Width = columnContentWidth(m.width, 1)
		content, starts, ends := m.renderThreadContent()
		m.cur().starts = starts
		m.cur().ends = ends
		m.cur().viewport.SetContent(content)
		return
	}
	width := m.visibleColumnWidth()
	m.cur().viewport.Width = width
	m.syncFeedColumn(m.cur(), width, true)
}

func (m *Model) syncFeedColumn(c *column, width int, focused bool) {
	content, starts, ends := m.renderFeedColumnContent(c, width, focused)
	c.starts = starts
	c.ends = ends
	c.viewport.SetContent(content)
}

func (m *Model) ensureSelectedVisible() {
	if m.cur().selected < 0 || m.cur().selected >= len(m.cur().starts) {
		return
	}
	// Keep a small margin above and below the selection so movement never
	// pins it to the viewport edge (vim's scrolloff).
	const margin = 2
	start := m.cur().starts[m.cur().selected]
	end := m.cur().ends[m.cur().selected]
	top := m.cur().viewport.YOffset
	if start-margin < top {
		top = max(0, start-margin)
	} else if end+margin >= top+m.cur().viewport.Height {
		top = end + margin - m.cur().viewport.Height + 1
	}
	// A post taller than the viewport anchors to its own first line.
	if start < top {
		top = start
	}
	maxTop := max(0, m.cur().ends[len(m.cur().ends)-1]+1-m.cur().viewport.Height)
	m.cur().viewport.YOffset = max(0, min(top, maxTop))
}

func (m Model) activePosts() []api.TimelinePost {
	if m.mode == modeThread {
		posts := make([]api.TimelinePost, len(m.cur().threadPosts))
		for i := range m.cur().threadPosts {
			posts[i] = m.cur().threadPosts[i].TimelinePost
		}
		return posts
	}
	return m.cur().posts
}

func (m Model) currentPost() (api.TimelinePost, bool) {
	if m.mode == modeThread {
		if m.cur().selected < 0 || m.cur().selected >= len(m.cur().threadPosts) {
			return api.TimelinePost{}, false
		}
		return m.cur().threadPosts[m.cur().selected].TimelinePost, true
	}
	if m.cur().selected < 0 || m.cur().selected >= len(m.cur().posts) {
		return api.TimelinePost{}, false
	}
	return m.cur().posts[m.cur().selected], true
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
	for columnIndex := range m.columns {
		c := &m.columns[columnIndex]
		for i := range c.posts {
			apply(&c.posts[i])
		}
		for i := range c.threadPosts {
			apply(&c.threadPosts[i].TimelinePost)
		}
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
