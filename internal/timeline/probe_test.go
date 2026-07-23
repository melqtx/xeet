package timeline

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestParseProbeReply(t *testing.T) {
	cases := []struct {
		name      string
		reply     string
		confirmed bool
		done      bool
	}{
		{"graphics ok then da1", "\x1b_Gi=4242;OK\x1b\\\x1b[?62;4c", true, true},
		{"da1 only, query ignored", "\x1b[?1;2c", false, true},
		{"graphics error then da1", "\x1b_Gi=4242;EBADF:no such file\x1b\\\x1b[?62c", false, true},
		{"ok but fence still pending", "\x1b_Gi=4242;OK\x1b\\", true, false},
		{"partial fence", "\x1b[?62;", false, false},
		{"empty", "", false, false},
	}
	for _, tc := range cases {
		confirmed, done := parseProbeReply([]byte(tc.reply))
		if confirmed != tc.confirmed || done != tc.done {
			t.Errorf("%s: parseProbeReply=%v,%v want %v,%v", tc.name, confirmed, done, tc.confirmed, tc.done)
		}
	}
}

func TestCtrlLTriggersRedraw(t *testing.T) {
	m := New()
	m.loading = false
	m.posts = mediaPosts(2)
	m.syncViewport()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if cmd == nil {
		t.Fatal("ctrl+l did not produce a redraw command")
	}
}
