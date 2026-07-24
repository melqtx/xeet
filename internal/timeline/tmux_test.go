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

func TestResolveImageModeInsideTmux(t *testing.T) {
	t.Setenv("ZELLIJ", "")
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
