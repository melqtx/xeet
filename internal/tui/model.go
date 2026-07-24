package tui

import (
	"context"
	"fmt"
	"strings"

	"xeet/internal/clip"
	"xeet/internal/media"
	"xeet/pkg/api"
	"xeet/pkg/config"

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

	return Model{width: 72, height: 22, editor: editor, pathInput: path, spinner: s, clipboard: clip, drafts: drafts}
}

func (m Model) Init() tea.Cmd { return textarea.Blink }

func Run() error {
	store, err := newFileDraftStore()
	if err != nil {
		return fmt.Errorf("initialize draft recovery: %w", err)
	}
	m := newWithDraftStore(systemClipboard{initErr: clip.Init()}, store)
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
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
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

func beginPost(text string, attachments []media.Attachment) tea.Cmd {
	return func() tea.Msg {
		events := make(chan tea.Msg, 8)
		ctx, cancel := context.WithCancel(context.Background())
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
				uploads = append(uploads, api.Upload{Filename: a.Name, ContentType: a.MIME, Data: a.Data})
			}
			client := api.NewWebClient(cfg)
			id, err := client.PostTweet(ctx, text, "", uploads, func(event api.PostEvent) {
				events <- postProgressMsg{event: event}
			})
			if client.ApplyRefreshedQueryIDs(cfg) {
				_ = mgr.Save(cfg)
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
	for _, existing := range m.attachments {
		if existing.ID == a.ID {
			m.toast = "That image is already attached"
			return
		}
	}
	m.attachments = append(m.attachments, a)
	m.selected = len(m.attachments) - 1
	m.resize()
	m.toast = fmt.Sprintf("Attached %s · %s · %dx%d · %s", a.Name, a.Format, a.Width, a.Height, media.HumanBytes(int(a.Size)))
}

func (m Model) hasDraft() bool {
	return strings.TrimSpace(m.editor.Value()) != "" || len(m.attachments) > 0
}
