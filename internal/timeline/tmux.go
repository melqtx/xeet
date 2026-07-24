package timeline

import (
	"os"
	"os/exec"
	"strings"
)

// insideTmux reports whether this pane sits behind a tmux server, which
// consumes escape sequences it does not recognize unless they arrive wrapped
// in its passthrough DCS.
func insideTmux() bool {
	return os.Getenv("TMUX") != ""
}

// tmuxWrap re-encodes an escape sequence as a tmux passthrough DCS
// (tmux 3.3+ with `allow-passthrough on`): tmux strips the wrapper, undoubles
// the ESC bytes, and forwards the payload to the outer terminal verbatim.
// Outside tmux the sequence is returned unchanged.
func tmuxWrap(seq string) string {
	if !insideTmux() {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// tmuxNativeSupport decides whether native kitty graphics can work from
// inside tmux. The interactive graphics probe is unreliable here — replies
// from the outer terminal are not routed back to the pane — so it checks the
// two hard requirements directly: the pane's session must allow passthrough,
// and the attached client's terminal must speak the kitty graphics protocol
// with Unicode placeholders (kitty and ghostty do; wezterm's kitty support
// lacks virtual placements). Returns ok plus a note explaining any refusal.
func tmuxNativeSupport() (bool, string) {
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return false, "tmux blocks native graphics; using ansi"
	}
	passthrough, err := exec.Command(tmuxBin, "show", "-Ap", "allow-passthrough").Output()
	if err != nil || !tmuxOptionEnabled(string(passthrough)) {
		return false, "native images need `tmux set -g allow-passthrough on`; using ansi"
	}
	clientTerm, err := exec.Command(tmuxBin, "display", "-p", "#{client_termname}").Output()
	if err != nil {
		return false, "tmux blocks native graphics; using ansi"
	}
	term := strings.ToLower(strings.TrimSpace(string(clientTerm)))
	if !strings.Contains(term, "kitty") && !strings.Contains(term, "ghostty") {
		return false, "the terminal running tmux has no kitty graphics; using ansi"
	}
	return true, ""
}

// tmuxOptionEnabled parses `tmux show` output such as "allow-passthrough on";
// "all" also enables passthrough (it additionally covers invisible panes).
func tmuxOptionEnabled(output string) bool {
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return false
	}
	value := fields[len(fields)-1]
	return value == "on" || value == "all"
}
