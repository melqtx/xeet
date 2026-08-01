package timeline

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
)

// playVideo hands the direct MP4 URL to mpv, which streams it without a full
// download first. mpv runs detached from xeet: its own window (or terminal
// UI, under --vo=tct/similar mpv configs) opens independently, and closing
// the player doesn't affect the timeline underneath.
func playVideo(videoURL string) tea.Cmd {
	return func() tea.Msg {
		if _, err := exec.LookPath("mpv"); err != nil {
			return actionMsg{err: fmt.Errorf("mpv not found; install mpv to play videos")}
		}
		cmd := exec.Command("mpv", "--force-window=immediate", videoURL)
		if err := cmd.Start(); err != nil {
			return actionMsg{err: fmt.Errorf("play video: %w", err)}
		}
		go func() { _ = cmd.Wait() }()
		return actionMsg{message: "playing in mpv"}
	}
}

func openExternalURL(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

func openReplyInX(postID, text string) tea.Cmd {
	return func() tea.Msg {
		return replyBrowserMsg{err: openExternalURL(replyIntentURL(postID, text))}
	}
}

func replyIntentURL(postID, text string) string {
	query := url.Values{
		"in_reply_to": []string{postID},
		"text":        []string{text},
	}
	return "https://x.com/intent/tweet?" + query.Encode()
}
