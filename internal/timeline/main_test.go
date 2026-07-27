package timeline

import (
	"os"
	"testing"
)

// The image mode is resolved from the environment, so without this the suite
// asserts against whichever terminal the developer happens to be running in:
// the same tests pass in Terminal.app and fail in Ghostty, kitty, WezTerm, and
// iTerm2. Individual tests already scrubbed parts of this — see the SSH
// variables in TestResolveImageModeInsideTmux — but a per-test list is only
// ever as complete as the detection was on the day it was written.
//
// TERM is pinned rather than unset because lipgloss derives its color profile
// from it, and an empty TERM would change rendered output everywhere.
func TestMain(m *testing.M) {
	os.Setenv("TERM", "xterm-256color")
	for _, key := range []string{
		"ZELLIJ", "TMUX",
		"TERM_PROGRAM", "LC_TERMINAL",
		"WEZTERM_PANE", "WEZTERM_EXECUTABLE",
		"ITERM_SESSION_ID", "KITTY_WINDOW_ID",
		"__CFBundleIdentifier",
		"SSH_CLIENT", "SSH_CONNECTION", "SSH_TTY",
	} {
		os.Unsetenv(key)
	}
	os.Exit(m.Run())
}
