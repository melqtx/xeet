package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xeet/internal/media"

	tea "github.com/charmbracelet/bubbletea"
)

const draftVersion = 1

type savedDraft struct {
	Version     int               `json:"version"`
	Text        string            `json:"text,omitempty"`
	Attachments []savedAttachment `json:"attachments,omitempty"`
}

type savedAttachment struct {
	Path string `json:"path"`
}

type draftStore interface {
	Load() (string, []media.Attachment, error)
	Save(string, []media.Attachment) error
	Clear() error
}

type saveDraftMsg struct{ seq int }

func (m *Model) scheduleDraftSave() tea.Cmd {
	m.draftSeq++
	seq := m.draftSeq
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return saveDraftMsg{seq: seq} })
}

func (m *Model) saveDraft() error {
	return m.drafts.Save(m.editor.Value(), append([]media.Attachment(nil), m.attachments...))
}

type memorylessDraftStore struct{}

func (memorylessDraftStore) Load() (string, []media.Attachment, error) { return "", nil, nil }
func (memorylessDraftStore) Save(string, []media.Attachment) error     { return nil }
func (memorylessDraftStore) Clear() error                              { return nil }

type fileDraftStore struct {
	path     string
	mediaDir string
}

func newFileDraftStore() (*fileDraftStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return newFileDraftStoreAt(home), nil
}

func newFileDraftStoreAt(home string) *fileDraftStore {
	return &fileDraftStore{
		path:     filepath.Join(home, ".xeet-draft.json"),
		mediaDir: filepath.Join(home, ".xeet-draft-media"),
	}
}

func (s *fileDraftStore) Load() (string, []media.Attachment, error) {
	data, err := readRegularFile(s.path)
	if os.IsNotExist(err) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("read saved draft: %w", err)
	}
	var draft savedDraft
	if err := json.Unmarshal(data, &draft); err != nil {
		return "", nil, fmt.Errorf("read saved draft: %w", err)
	}
	if draft.Version != draftVersion {
		return "", nil, fmt.Errorf("saved draft has unsupported version %d", draft.Version)
	}
	attachments := make([]media.Attachment, 0, len(draft.Attachments))
	var skipped int
	for _, saved := range draft.Attachments {
		if len(attachments) == media.MaxAttachments {
			break
		}
		attachment, loadErr := media.FromPath(saved.Path)
		if loadErr != nil {
			skipped++
			continue
		}
		attachments = append(attachments, attachment)
	}
	if skipped > 0 {
		return draft.Text, attachments, fmt.Errorf("%d saved image attachment(s) are no longer available", skipped)
	}
	return draft.Text, attachments, nil
}

func (s *fileDraftStore) Save(text string, attachments []media.Attachment) error {
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
		return s.Clear()
	}
	if err := rejectSymlink(s.path); err != nil {
		return err
	}

	draft := savedDraft{Version: draftVersion, Text: text}
	keep := make(map[string]bool)
	for _, attachment := range attachments {
		path := attachment.Path
		if attachment.Source == media.SourceClipboard || path == "" {
			var err error
			path, err = s.saveClipboardAttachment(attachment)
			if err != nil {
				return err
			}
		}
		if filepath.Dir(path) == s.mediaDir {
			keep[path] = true
		}
		draft.Attachments = append(draft.Attachments, savedAttachment{Path: path})
	}
	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := atomicWrite0600(s.path, data); err != nil {
		return fmt.Errorf("save draft: %w", err)
	}
	return s.cleanMedia(keep)
}

func (s *fileDraftStore) saveClipboardAttachment(attachment media.Attachment) (string, error) {
	if len(attachment.Data) == 0 {
		return "", fmt.Errorf("save clipboard attachment %q: image data is empty", attachment.Name)
	}
	if err := ensurePrivateDirectory(s.mediaDir); err != nil {
		return "", fmt.Errorf("create draft media directory: %w", err)
	}
	ext := strings.ToLower(attachment.Format)
	if ext == "jpeg" {
		ext = "jpg"
	}
	if ext == "" {
		ext = "image"
	}
	path := filepath.Join(s.mediaDir, attachment.ID+"."+ext)
	if err := rejectSymlink(path); err != nil {
		return "", err
	}
	if err := atomicWrite0600(path, attachment.Data); err != nil {
		return "", fmt.Errorf("save clipboard attachment: %w", err)
	}
	return path, nil
}

func (s *fileDraftStore) Clear() error {
	var result error
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		result = errors.Join(result, err)
	}
	if err := os.RemoveAll(s.mediaDir); err != nil {
		result = errors.Join(result, err)
	}
	return result
}

func (s *fileDraftStore) cleanMedia(keep map[string]bool) error {
	entries, err := os.ReadDir(s.mediaDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(s.mediaDir, entry.Name())
		if !keep[path] {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return os.Mkdir(path, 0700)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is not a directory; refusing to use it", path)
	}
	return os.Chmod(path, 0700)
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return os.ReadFile(path)
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to replace it", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

func atomicWrite0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".xeet-draft-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
