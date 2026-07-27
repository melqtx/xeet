package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/melqtx/xeet/internal/clip"
	"github.com/melqtx/xeet/internal/media"
	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type screen int
type dialog int
type focus int

const (
	screenCompose screen = iota
	screenPosting
	screenSuccess
)

const (
	dialogNone dialog = iota
	dialogPath
	dialogHelp
	dialogQuit
)

const (
	focusEditor focus = iota
	focusAttachments
)

type clipboardReader interface {
	ReadImage() []byte
	ReadText() string
}

type systemClipboard struct{ initErr error }

func (c systemClipboard) ClipboardError() error { return c.initErr }

func (c systemClipboard) ReadImage() []byte {
	if c.initErr != nil {
		return nil
	}
	return clip.ReadImage()
}
func (c systemClipboard) ReadText() string {
	if c.initErr != nil {
		return ""
	}
	return clip.ReadText()
}

type Model struct {
	// ctx is the parent of the posting request, so an interrupt cancels an
	// upload in flight rather than detaching it from the program.
	ctx            context.Context
	width, height  int
	screen         screen
	dialog         dialog
	focus          focus
	editor         textarea.Model
	pathInput      textinput.Model
	spinner        spinner.Model
	attachments    []media.Attachment
	selected       int
	toast          string
	lastErr        error
	postDiagnostic string
	postStage      api.PostEvent
	postEvents     <-chan tea.Msg
	postCancel     context.CancelFunc
	cancelling     bool
	postID         string
	clipboard      clipboardReader
	drafts         draftStore
	draftSeq       int
}

func New(clip clipboardReader) Model {
	return newWithDraftStore(clip, memorylessDraftStore{})
}

func newWithDraftStore(clip clipboardReader, drafts draftStore) Model {
	if clip == nil {
		clip = systemClipboard{initErr: fmt.Errorf("clipboard unavailable")}
	}
	if drafts == nil {
		drafts = memorylessDraftStore{}
	}
	editor := textarea.New()
	editor.Placeholder = "what are you thinking?"
	editor.Prompt = ""
	editor.CharLimit = 280
	editor.SetWidth(56)
	editor.SetHeight(7)
	editor.ShowLineNumbers = false
	editor.Focus()

	path := textinput.New()
	path.Placeholder = "~/Desktop/photo.png"
	path.Prompt = "> "
	path.CharLimit = 4096

	s := spinner.New()
	s.Spinner = spinner.Dot

	return Model{
		ctx: context.Background(), width: 72, height: 22,
		editor: editor, pathInput: path, spinner: s, clipboard: clip, drafts: drafts,
	}
}

func (m Model) Init() tea.Cmd { return textarea.Blink }

// requestContext is the parent for the posting request. Models built directly
// in tests may leave ctx nil.
func (m Model) requestContext() context.Context {
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func Run(ctx context.Context) error {
	store, err := newFileDraftStore()
	if err != nil {
		return fmt.Errorf("initialize draft recovery: %w", err)
	}
	m := newWithDraftStore(systemClipboard{initErr: clip.Init()}, store)
	m.ctx = ctx
	text, attachments, restoreErr := store.Load()
	if text != "" || len(attachments) > 0 {
		m.editor.SetValue(text)
		m.attachments = attachments
		m.toast = "Restored your saved draft"
		m.resize()
	}
	if restoreErr != nil {
		m.toast = restoreErr.Error()
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if errors.Is(err, tea.ErrProgramKilled) {
		// An interrupt reached the program through ctx; the draft is already
		// autosaved, so this is an ordinary exit.
		return nil
	}
	return err
}

type clipboardMsg struct {
	attachment *media.Attachment
	text       string
	err        error
}
type attachmentMsg struct {
	attachment media.Attachment
	err        error
}
type postStartedMsg struct {
	events <-chan tea.Msg
	cancel context.CancelFunc
}
type postProgressMsg struct{ event api.PostEvent }
type postResultMsg struct {
	id         string
	err        error
	diagnostic string
}

func readClipboard(clip clipboardReader) tea.Cmd {
	return func() tea.Msg {
		if status, ok := clip.(interface{ ClipboardError() error }); ok && status.ClipboardError() != nil {
			return clipboardMsg{err: fmt.Errorf("system clipboard unavailable; paste text normally or use Ctrl+O to attach an image file: %w", status.ClipboardError())}
		}
		if data := clip.ReadImage(); len(data) > 0 {
			a, err := media.FromClipboard(data)
			return clipboardMsg{attachment: &a, err: err}
		}
		if text := clip.ReadText(); text != "" {
			return clipboardMsg{text: text}
		}
		return clipboardMsg{err: fmt.Errorf("clipboard does not contain an image or text")}
	}
}

func loadAttachment(path string) tea.Cmd {
	return func() tea.Msg { a, err := media.FromPath(path); return attachmentMsg{attachment: a, err: err} }
}

func beginPost(parent context.Context, text string, attachments []media.Attachment) tea.Cmd {
	return func() tea.Msg {
		events := make(chan tea.Msg, 8)
		ctx, cancel := context.WithCancel(parent)
		go func() {
			defer cancel()
			mgr, err := config.NewConfigManager()
			if err != nil {
				events <- postResultMsg{err: err}
				close(events)
				return
			}
			cfg, err := mgr.Load()
			if err != nil {
				events <- postResultMsg{err: err}
				close(events)
				return
			}
			if cfg.AuthToken == "" {
				events <- postResultMsg{err: fmt.Errorf("run 'xeet auth' first")}
				close(events)
				return
			}
			uploads := make([]api.Upload, 0, len(attachments))
			for _, a := range attachments {
				upload := api.Upload{Filename: a.Name, ContentType: a.MIME, Data: a.Data}
				if a.IsVideo() {
					upload.Path = a.Path
				}
				uploads = append(uploads, upload)
			}
			client := api.NewWebClient(cfg)
			id, err := client.PostTweet(ctx, text, "", uploads, func(event api.PostEvent) {
				events <- postProgressMsg{event: event}
			})
			if client.ApplyRefreshedQueryIDs(cfg) {
				_ = mgr.SaveQueryIDs(cfg)
			}
			events <- postResultMsg{id: id, err: err, diagnostic: client.LastDiagnostic()}
			close(events)
		}()
		return postStartedMsg{events: events, cancel: cancel}
	}
}

func waitForPost(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return postResultMsg{err: fmt.Errorf("posting stopped unexpectedly")}
		}
		return msg
	}
}

func (m *Model) addAttachment(a media.Attachment) {
	if len(m.attachments) >= media.MaxAttachments {
		m.toast = fmt.Sprintf("Maximum of %d images", media.MaxAttachments)
		return
	}
	// X allows one video per post, never mixed with images.
	if a.IsVideo() && len(m.attachments) > 0 {
		m.toast = "A video must be the only attachment"
		return
	}
	if !a.IsVideo() && len(m.attachments) > 0 && m.attachments[0].IsVideo() {
		m.toast = "Images can't join a video post"
		return
	}
	for _, existing := range m.attachments {
		if existing.ID == a.ID {
			m.toast = "That file is already attached"
			return
		}
	}
	m.attachments = append(m.attachments, a)
	m.selected = len(m.attachments) - 1
	m.resize()
	if a.IsVideo() {
		m.toast = fmt.Sprintf("Attached %s · %s · %s", a.Name, a.Format, media.HumanBytes(int(a.Size)))
		return
	}
	m.toast = fmt.Sprintf("Attached %s · %s · %dx%d · %s", a.Name, a.Format, a.Width, a.Height, media.HumanBytes(int(a.Size)))
}

func (m Model) hasDraft() bool {
	return strings.TrimSpace(m.editor.Value()) != "" || len(m.attachments) > 0
}
