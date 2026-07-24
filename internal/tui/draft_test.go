package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/melqtx/xeet/internal/media"

	tea "github.com/charmbracelet/bubbletea"
)

func writeDraftPNG(t *testing.T, dir, name string, width int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pngBytes(t, width), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFileDraftStoreRoundTrip(t *testing.T) {
	home := t.TempDir()
	imagePath := writeDraftPNG(t, home, "photo.png", 7)
	attachment, err := media.FromPath(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	store := newFileDraftStoreAt(home)
	if err := store.Save("unfinished words", []media.Attachment{attachment}); err != nil {
		t.Fatal(err)
	}

	text, attachments, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if text != "unfinished words" || len(attachments) != 1 || attachments[0].Width != 7 {
		t.Fatalf("restored text=%q attachments=%+v", text, attachments)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("draft permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestClipboardDraftAttachmentIsPersistedAndCleaned(t *testing.T) {
	home := t.TempDir()
	attachment, err := media.FromClipboard(pngBytes(t, 5))
	if err != nil {
		t.Fatal(err)
	}
	store := newFileDraftStoreAt(home)
	if err := store.Save("clipboard pic", []media.Attachment{attachment}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(store.mediaDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("draft media entries=%v err=%v", entries, err)
	}
	mediaDirInfo, err := os.Stat(store.mediaDir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && mediaDirInfo.Mode().Perm() != 0700 {
		t.Fatalf("draft media permissions = %o, want 700", mediaDirInfo.Mode().Perm())
	}

	text, attachments, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if text != "clipboard pic" || len(attachments) != 1 || attachments[0].Width != 5 {
		t.Fatalf("restored text=%q attachments=%+v", text, attachments)
	}
	// Saving a restored clipboard sidecar must not delete the file referenced
	// by the newly written draft.
	if err := store.Save(text, attachments); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(attachments[0].Path); err != nil {
		t.Fatalf("restored clipboard sidecar was removed: %v", err)
	}

	// The in-memory clipboard attachment remains marked as clipboard-sourced.
	// Later text autosaves must reuse its content-addressed sidecar rather than
	// rewriting and syncing the full image each time.
	sidecar := filepath.Join(store.mediaDir, attachment.ID+".png")
	oldTime := time.Unix(1_600_000_000, 0)
	if err := os.Chtimes(sidecar, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("edited text", []media.Attachment{attachment}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(oldTime) {
		t.Fatalf("clipboard sidecar was rewritten: mtime=%v", info.ModTime())
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.path); !os.IsNotExist(err) {
		t.Fatalf("draft file remains after clear: %v", err)
	}
	if _, err := os.Stat(store.mediaDir); !os.IsNotExist(err) {
		t.Fatalf("draft media remains after clear: %v", err)
	}
}

func TestDraftLoadPreservesTextWhenAttachmentDisappears(t *testing.T) {
	home := t.TempDir()
	path := writeDraftPNG(t, home, "temporary.png", 3)
	attachment, err := media.FromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	store := newFileDraftStoreAt(home)
	if err := store.Save("keep the words", []media.Attachment{attachment}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	text, attachments, err := store.Load()
	if text != "keep the words" || len(attachments) != 0 || err == nil || !strings.Contains(err.Error(), "no longer available") {
		t.Fatalf("text=%q attachments=%d err=%v", text, len(attachments), err)
	}
}

func TestDraftStoreRefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink setup requires privileges on Windows")
	}
	home := t.TempDir()
	target := filepath.Join(home, "target")
	if err := os.WriteFile(target, []byte("do not replace"), 0600); err != nil {
		t.Fatal(err)
	}
	store := newFileDraftStoreAt(home)
	if err := os.Symlink(target, store.path); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("secret draft", nil); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink refusal, got %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "do not replace" {
		t.Fatalf("symlink target changed: %q, %v", data, err)
	}
}

type recordingDraftStore struct {
	text        string
	attachments []media.Attachment
	saves       int
	clears      int
}

func (s *recordingDraftStore) Load() (string, []media.Attachment, error) {
	return s.text, s.attachments, nil
}
func (s *recordingDraftStore) Save(text string, attachments []media.Attachment) error {
	s.text = text
	s.attachments = attachments
	s.saves++
	return nil
}
func (s *recordingDraftStore) Clear() error { s.clears++; return nil }

func TestDraftAutosaveDebouncesAndSuccessClears(t *testing.T) {
	store := &recordingDraftStore{}
	m := newWithDraftStore(fakeClipboard{}, store)
	m.editor.SetValue("first")
	first := m.scheduleDraftSave()
	m.editor.SetValue("latest")
	second := m.scheduleDraftSave()

	m = updateModel(t, m, first())
	if store.saves != 0 {
		t.Fatal("stale autosave was written")
	}
	m = updateModel(t, m, second())
	if store.saves != 1 || store.text != "latest" {
		t.Fatalf("saves=%d text=%q", store.saves, store.text)
	}

	m.screen = screenPosting
	m = updateModel(t, m, postResultMsg{id: "123"})
	if store.clears != 1 || m.screen != screenSuccess {
		t.Fatalf("clears=%d screen=%v", store.clears, m.screen)
	}
}

func TestEmptyComposerClearsAPreviouslySavedDraft(t *testing.T) {
	store := &recordingDraftStore{text: "stale"}
	m := newWithDraftStore(fakeClipboard{}, store)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_ = next.(Model)
	if cmd == nil || store.clears != 1 {
		t.Fatalf("quit command=%v clears=%d", cmd, store.clears)
	}
}

func TestQuitDialogCanSaveOrDiscardDraft(t *testing.T) {
	store := &recordingDraftStore{}
	m := newWithDraftStore(fakeClipboard{}, store)
	m.editor.SetValue("come back later")
	m.dialog = dialogQuit
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || store.saves != 1 || store.text != "come back later" {
		t.Fatalf("save quit command=%v saves=%d text=%q", cmd, store.saves, store.text)
	}

	m.dialog = dialogQuit
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	_ = next.(Model)
	if cmd == nil || store.clears != 1 {
		t.Fatalf("discard quit command=%v clears=%d", cmd, store.clears)
	}
}
