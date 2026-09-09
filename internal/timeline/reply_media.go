package timeline

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/melqtx/xeet/internal/clip"
	"github.com/melqtx/xeet/internal/media"
)

type replyDraft struct {
	text        string
	attachments []media.Attachment
}
type replyMediaMsg struct {
	seq        int
	attachment *media.Attachment
	text       string
	err        error
}

func (m *Model) loadReplyMedia(path string, clipboard bool) tea.Cmd {
	m.replyMediaLoading = true
	m.replyMediaSeq++
	seq, clipboardOK := m.replyMediaSeq, m.clipboardOK
	return func() tea.Msg {
		msg := replyMediaMsg{seq: seq}
		var attachment media.Attachment
		if clipboard {
			if !clipboardOK {
				msg.err = fmt.Errorf("clipboard unavailable; use ctrl+o to attach a file")
				return msg
			}
			if data := clip.ReadImage(); len(data) > 0 {
				attachment, msg.err = media.FromClipboard(data)
			} else {
				msg.text = clip.ReadText()
				if msg.text == "" {
					msg.err = fmt.Errorf("clipboard has no image or text")
				}
				return msg
			}
		} else {
			attachment, msg.err = media.FromPath(path)
		}
		if msg.err == nil {
			msg.attachment = &attachment
		}
		return msg
	}
}

func (m *Model) addReplyAttachment(a media.Attachment) error {
	if a.IsVideo() {
		return fmt.Errorf("replies currently support images; attach a photo or GIF")
	}
	if len(m.replyAttachments) >= media.MaxAttachments {
		return fmt.Errorf("maximum of 4 images")
	}
	for _, existing := range m.replyAttachments {
		if existing.ID == a.ID {
			return fmt.Errorf("that image is already attached")
		}
	}
	m.replyAttachments = append(m.replyAttachments, a)
	m.replyAttachmentSelected = len(m.replyAttachments) - 1
	m.resize()
	return nil
}

func (m *Model) saveReplyDraft() {
	if m.replyDrafts == nil {
		m.replyDrafts = map[string]replyDraft{}
	}
	m.replyDrafts[m.replyDraftKey()] = replyDraft{m.replyEditor.Value(), append([]media.Attachment(nil), m.replyAttachments...)}
}

func (m Model) replyDraftKey() string {
	if m.replyQuote {
		return "quote:" + m.replyPost.ID
	}
	return m.replyPost.ID
}
