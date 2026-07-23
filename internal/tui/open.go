package tui

import (
	"net/url"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
)

type browserOpenedMsg struct{ err error }

func openDraftInX(text string) tea.Cmd {
	return func() tea.Msg {
		return browserOpenedMsg{err: openExternalURL(postIntentURL(text))}
	}
}

func postIntentURL(text string) string {
	query := url.Values{"text": []string{text}}
	return "https://x.com/intent/tweet?" + query.Encode()
}

func openExternalURL(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start()
}
