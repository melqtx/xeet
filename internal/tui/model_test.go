package tui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"

	"xeet/internal/media"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeClipboard struct {
	image []byte
	text  string
}

func (f fakeClipboard) ReadImage() []byte { return f.image }
func (f fakeClipboard) ReadText() string  { return f.text }

func pngBytes(t *testing.T, width int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, width, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func updateModel(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestQTypesInsteadOfQuitting(t *testing.T) {
	m := New(fakeClipboard{})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if m.editor.Value() != "q" {
		t.Fatalf("q was not inserted: %q", m.editor.Value())
	}
}

func TestAltEnterCreatesNewline(t *testing.T) {
	m := New(fakeClipboard{})
	m.editor.SetValue("hello")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if m.editor.Value() != "hello\n" {
		t.Fatalf("alt+enter did not insert newline: %q", m.editor.Value())
	}
}

func TestOnlyEnterSubmits(t *testing.T) {
	m := New(fakeClipboard{})
	m.editor.SetValue("hello")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.screen != screenCompose {
		t.Fatalf("ctrl+s unexpectedly submitted")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenPosting || m.editor.Value() != "hello" {
		t.Fatalf("unexpected submit state: screen=%v value=%q", m.screen, m.editor.Value())
	}
}

func TestAttachmentLimitAndDuplicates(t *testing.T) {
	m := New(fakeClipboard{})
	first, err := media.FromClipboard(pngBytes(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	m.addAttachment(first)
	m.addAttachment(first)
	if len(m.attachments) != 1 {
		t.Fatalf("duplicate was added")
	}
	for i := 2; i <= 5; i++ {
		a, err := media.FromClipboard(pngBytes(t, i))
		if err != nil {
			t.Fatal(err)
		}
		m.addAttachment(a)
	}
	if len(m.attachments) != media.MaxAttachments {
		t.Fatalf("got %d attachments", len(m.attachments))
	}
	if m.toast != "Maximum of 4 images" {
		t.Fatalf("unexpected toast %q", m.toast)
	}
}

func TestPostErrorPreservesDraft(t *testing.T) {
	m := New(fakeClipboard{})
	m.editor.SetValue("keep me")
	m.screen = screenPosting
	m = updateModel(t, m, postResultMsg{err: errors.New("nope")})
	if m.screen != screenCompose || m.editor.Value() != "keep me" || m.lastErr == nil {
		t.Fatalf("draft was not preserved: %+v", m)
	}
}

func TestClipboardImageBecomesAttachment(t *testing.T) {
	cmd := readClipboard(fakeClipboard{image: pngBytes(t, 3), text: "ignored"})
	msg := cmd()
	result, ok := msg.(clipboardMsg)
	if !ok || result.err != nil || result.attachment == nil || result.attachment.Width != 3 {
		t.Fatalf("unexpected clipboard result: %#v", msg)
	}
}

func TestCancelPostingDoesNotQuit(t *testing.T) {
	cancelled := false
	m := New(fakeClipboard{})
	m.screen = screenPosting
	m.postCancel = func() { cancelled = true }
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if cmd != nil || !cancelled || !m.cancelling || m.screen != screenPosting {
		t.Fatalf("posting was not cancelled safely")
	}
}

func TestResizeBoundsEditor(t *testing.T) {
	m := New(fakeClipboard{})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 44, Height: 15})
	if m.editor.Width() < 24 || m.editor.Height() < 4 {
		t.Fatalf("bad compact dimensions: %dx%d", m.editor.Width(), m.editor.Height())
	}
}

func TestDraftPreservedOnPostFailure(t *testing.T) {
	// Regression guard: every posting failure must return to the composer
	// with the draft text and attachments intact.
	m := New(fakeClipboard{})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.editor.SetValue("my precious draft")
	m.attachments = append(m.attachments, media.Attachment{Name: "pic.png", MIME: "image/png", Data: pngBytes(t, 4)})
	m.screen = screenPosting

	m = updateModel(t, m, postResultMsg{err: errors.New("x is down")})

	if m.screen != screenCompose {
		t.Fatalf("screen = %v, want screenCompose", m.screen)
	}
	if m.editor.Value() != "my precious draft" {
		t.Fatalf("draft lost: %q", m.editor.Value())
	}
	if len(m.attachments) != 1 {
		t.Fatalf("attachments lost: %d", len(m.attachments))
	}
	if m.lastErr == nil {
		t.Fatal("error not surfaced to the user")
	}
}

func TestDraftClearedOnCancelNotOnFailure(t *testing.T) {
	m := New(fakeClipboard{})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.editor.SetValue("still here")
	m.screen = screenPosting

	m = updateModel(t, m, postResultMsg{err: context.Canceled})

	if m.screen != screenCompose {
		t.Fatalf("screen = %v, want screenCompose", m.screen)
	}
	if m.editor.Value() != "still here" {
		t.Fatalf("draft lost on cancel: %q", m.editor.Value())
	}
	if m.lastErr != nil {
		t.Fatalf("cancel should not surface an error, got %v", m.lastErr)
	}
}
