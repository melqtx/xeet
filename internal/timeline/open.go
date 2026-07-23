package timeline

import (
	"net/url"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
)

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
