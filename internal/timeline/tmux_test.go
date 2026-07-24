package timeline

import (
	"strings"
	"testing"
)

func TestTmuxWrapOutsideTmuxIsIdentity(t *testing.T) {
	t.Setenv("TMUX", "")
	seq := "\x1b_Ga=p,U=1,i=7\x1b\\"
	if got := tmuxWrap(seq); got != seq {
		t.Fatalf("expected identity outside tmux, got %q", got)
	}
}

func TestTmuxWrapDoublesEscapes(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-0/default,123,0")
	got := tmuxWrap("\x1b_Gq=2\x1b\\")
	want := "\x1bPtmux;\x1b\x1b_Gq=2\x1b\x1b\\\x1b\\"
	if got != want {
		t.Fatalf("wrapped sequence = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, "\x1bPtmux;") || !strings.HasSuffix(got, "\x1b\\") {
		t.Fatalf("missing DCS wrapper: %q", got)
	}
}

func TestTmuxOptionEnabled(t *testing.T) {
	cases := map[string]bool{
		"allow-passthrough on\n":  true,
		"allow-passthrough all\n": true,
		"allow-passthrough off\n": false,
		"on\n":                    true,
		"":                        false,
	}
	for output, want := range cases {
		if got := tmuxOptionEnabled(output); got != want {
			t.Errorf("tmuxOptionEnabled(%q) = %v, want %v", output, got, want)
		}
	}
}

// A remote tmux reports the ssh client's terminal name, so the capability
// check would otherwise green-light native mode for previews whose temp
// files that terminal can never open.
func TestTmuxNativeSupportRefusesOverSSH(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-0/default,1,0")
	t.Setenv("SSH_CONNECTION", "10.0.0.2 51000 10.0.0.1 22")
	ok, note := tmuxNativeSupport()
	if ok || note == "" {
		t.Fatalf("tmuxNativeSupport over ssh = (%v, %q), want refusal with a note", ok, note)
	}
	if got, _ := resolveImageMode("auto"); got != imageModeANSI {
		t.Fatalf("auto over ssh+tmux = %v, want ansi", got)
	}
}

func TestResolveImageModeInsideTmux(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	// Cleared so the assertions below fail for the reason they claim even
	// when the suite itself runs over ssh.
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CLIENT", "")
	// A bogus socket path makes every tmux server query fail, which must
	// downgrade auto mode to ansi rather than error or claim native.
	t.Setenv("TMUX", "/nonexistent/socket,999999,0")
	if got, note := resolveImageMode("auto"); got != imageModeANSI || note == "" {
		t.Fatalf("auto in unverifiable tmux = %v (%q), want ansi with note", got, note)
	}
	if got, _ := resolveImageMode("native"); got != imageModeNative {
		t.Fatalf("explicit native must bypass the tmux checks")
	}
	if got, _ := resolveImageMode("off"); got != imageModeOff {
		t.Fatalf("off must stay off inside tmux")
	}
}
