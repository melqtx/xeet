package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xeet/internal/media"
	"xeet/pkg/api"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case saveDraftMsg:
		if msg.seq != m.draftSeq {
			return m, nil
		}
		if err := m.saveDraft(); err != nil {
			m.toast = "Couldn't autosave draft: " + err.Error()
		}
		return m, nil
	case clipboardMsg:
		if msg.err != nil {
			m.lastErr = msg.err
			return m, nil
		}
		m.lastErr = nil
		if msg.attachment != nil {
			m.addAttachment(*msg.attachment)
		} else if msg.text != "" {
			m.editor.InsertString(msg.text)
			m.toast = "Pasted text"
		}
		return m, m.scheduleDraftSave()
	case attachmentMsg:
		if msg.err != nil {
			m.lastErr = msg.err
			return m, nil
		}
		m.lastErr = nil
		m.addAttachment(msg.attachment)
		return m, m.scheduleDraftSave()
	case postStartedMsg:
		m.postEvents = msg.events
		m.postCancel = msg.cancel
		return m, tea.Batch(m.spinner.Tick, waitForPost(msg.events))
	case postProgressMsg:
		m.postStage = msg.event
		return m, waitForPost(m.postEvents)
	case postResultMsg:
		m.postCancel = nil
		m.cancelling = false
		m.postDiagnostic = msg.diagnostic
		if msg.err != nil {
			m.screen = screenCompose
			if errors.Is(msg.err, context.Canceled) {
				m.lastErr = nil
				m.toast = "Posting cancelled"
			} else {
				m.lastErr = msg.err
			}
			m.editor.Focus()
			return m, nil
		}
		m.screen = screenSuccess
		m.postID = msg.id
		m.postDiagnostic = ""
		m.postEvents = nil
		m.draftSeq++
		if err := m.drafts.Clear(); err != nil {
			m.toast = "Warning: couldn't clear the saved draft: " + err.Error()
		} else {
			m.toast = ""
		}
		return m, nil
	case browserOpenedMsg:
		if msg.err != nil {
			m.lastErr = fmt.Errorf("couldn't open X: %w", msg.err)
			return m, nil
		}
		m.lastErr = nil
		m.postDiagnostic = ""
		m.toast = "Opened your draft in X"
		return m, nil
	case spinner.TickMsg:
		if m.screen == screenPosting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	key, isKey := msg.(tea.KeyMsg)
	if isKey {
		if m.dialog != dialogNone {
			return m.updateDialog(key)
		}
		if m.screen == screenSuccess {
			return m.updateSuccess(key)
		}
		if m.screen == screenPosting {
			if (key.String() == "ctrl+c" || key.String() == "esc") && m.postCancel != nil && !m.cancelling {
				m.cancelling = true
				m.postCancel()
			}
			return m, nil
		}

		switch key.String() {
		case "b":
			if canOpenDraftInX(m.lastErr) && strings.TrimSpace(m.editor.Value()) != "" {
				return m, openDraftInX(m.editor.Value())
			}
		case "ctrl+c", "esc":
			if m.hasDraft() {
				m.dialog = dialogQuit
				return m, nil
			}
			m.draftSeq++
			if err := m.drafts.Clear(); err != nil {
				m.lastErr = fmt.Errorf("clear empty draft: %w", err)
				return m, nil
			}
			return m, tea.Quit
		case "enter":
			if !m.hasDraft() {
				m.toast = "Write something or attach an image first"
				return m, nil
			}
			if err := m.saveDraft(); err != nil {
				m.lastErr = fmt.Errorf("save draft before posting: %w", err)
				return m, nil
			}
			m.lastErr = nil
			m.postDiagnostic = ""
			m.toast = ""
			m.screen = screenPosting
			m.postStage = api.PostEvent{}
			m.cancelling = false
			m.editor.Blur()
			attachments := append([]media.Attachment(nil), m.attachments...)
			return m, beginPost(m.editor.Value(), attachments)
		case "alt+enter", "ctrl+j":
			if m.focus == focusEditor {
				m.editor.InsertString("\n")
				return m, m.scheduleDraftSave()
			}
			return m, nil
		case "ctrl+v":
			return m, readClipboard(m.clipboard)
		case "ctrl+o":
			m.dialog = dialogPath
			m.pathInput.SetValue("")
			return m, m.pathInput.Focus()
		case "f1":
			m.dialog = dialogHelp
			return m, nil
		case "tab":
			if len(m.attachments) > 0 {
				if m.focus == focusEditor {
					m.focus = focusAttachments
					m.editor.Blur()
				} else {
					m.focus = focusEditor
					return m, m.editor.Focus()
				}
			}
			return m, nil
		}

		if m.focus == focusAttachments {
			switch key.String() {
			case "left", "up", "shift+tab":
				if m.selected > 0 {
					m.selected--
				}
			case "right", "down":
				if m.selected < len(m.attachments)-1 {
					m.selected++
				}
			case "backspace", "delete", "ctrl+x":
				m.removeSelected()
				return m, m.scheduleDraftSave()
			case "enter":
				m.focus = focusEditor
				return m, m.editor.Focus()
			}
			return m, nil
		}
	}

	before := m.editor.Value()
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	cmds = append(cmds, cmd)
	if m.editor.Value() != before {
		cmds = append(cmds, m.scheduleDraftSave())
	}
	return m, tea.Batch(cmds...)
}

func canOpenDraftInX(err error) bool {
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

func (m Model) updateDialog(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.dialog {
	case dialogQuit:
		switch strings.ToLower(key.String()) {
		case "y", "enter", "ctrl+c":
			if err := m.saveDraft(); err != nil {
				m.dialog = dialogNone
				m.lastErr = fmt.Errorf("save draft before quitting: %w", err)
				return m, nil
			}
			return m, tea.Quit
		case "d":
			if err := m.drafts.Clear(); err != nil {
				m.dialog = dialogNone
				m.lastErr = fmt.Errorf("discard saved draft: %w", err)
				return m, nil
			}
			return m, tea.Quit
		case "n", "esc":
			m.dialog = dialogNone
		}
		return m, nil
	case dialogHelp:
		if key.String() == "esc" || key.String() == "f1" || key.String() == "enter" {
			m.dialog = dialogNone
		}
		return m, nil
	case dialogPath:
		switch key.String() {
		case "esc":
			m.dialog = dialogNone
			m.pathInput.Blur()
			return m, m.editor.Focus()
		case "enter":
			path := m.pathInput.Value()
			m.dialog = dialogNone
			m.pathInput.Blur()
			m.focus = focusEditor
			m.editor.Focus()
			return m, loadAttachment(path)
		}
		var cmd tea.Cmd
		m.pathInput, cmd = m.pathInput.Update(key)
		return m, cmd
	}
	return m, nil
}

func (m Model) updateSuccess(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "q" || key.String() == "esc" || key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if key.String() == "enter" || key.String() == "n" {
		m.screen = screenCompose
		m.editor.Reset()
		m.attachments = nil
		m.selected = 0
		m.postID = ""
		m.toast = ""
		m.lastErr = nil
		m.postDiagnostic = ""
		m.focus = focusEditor
		return m, m.editor.Focus()
	}
	return m, nil
}

func (m *Model) removeSelected() {
	if len(m.attachments) == 0 {
		return
	}
	name := m.attachments[m.selected].Name
	m.attachments = append(m.attachments[:m.selected], m.attachments[m.selected+1:]...)
	if m.selected >= len(m.attachments) && m.selected > 0 {
		m.selected--
	}
	m.toast = "Removed " + name
	m.resize()
	if len(m.attachments) == 0 {
		m.focus = focusEditor
		m.editor.Focus()
	}
}

func (m *Model) resize() {
	width := m.contentWidth() - 4
	if width < 24 {
		width = 24
	}
	height := m.height - 18 - len(m.attachments)
	if height < 4 {
		height = 4
	}
	if height > 8 {
		height = 8
	}
	m.editor.SetWidth(width)
	m.editor.SetHeight(height)
	m.pathInput.Width = width - 4
}
