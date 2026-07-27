//go:build darwin || linux

package tui

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type ptyComposerModel struct{ Model }

func (m ptyComposerModel) Init() tea.Cmd { return nil }

func (m ptyComposerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.Model.Update(msg)
	return ptyComposerModel{Model: next.(Model)}, cmd
}

func (m ptyComposerModel) View() string { return m.Model.View() }

func TestComposerPTYHelper(t *testing.T) {
	if os.Getenv("XEET_COMPOSER_PTY_HELPER") != "1" {
		return
	}
	// The child starts with TERM=dumb so Bubble Tea's package initializer
	// cannot consume scripted input while probing a bare PTY. Restore the
	// production terminal identity before exercising the composer itself.
	t.Setenv("TERM", "xterm-256color")
	m := New(fakeClipboard{})
	if _, err := tea.NewProgram(ptyComposerModel{Model: m}, tea.WithAltScreen(), tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run(); err != nil {
		t.Fatal(err)
	}
}

func TestComposerPTYUnicodeResizeAndDraftDialog(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestComposerPTYHelper$")
	cmd.Env = append(os.Environ(), "XEET_COMPOSER_PTY_HELPER=1", "TERM=dumb", "NO_COLOR=1")
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()

	var captured lockedBuffer
	go func() { _, _ = io.Copy(&captured, terminal) }()
	write := func(value string, pause time.Duration) {
		t.Helper()
		if _, err := terminal.WriteString(value); err != nil {
			t.Fatal(err)
		}
		time.Sleep(pause)
	}

	time.Sleep(120 * time.Millisecond)
	write("hello 🐈 café", 150*time.Millisecond)
	if err := pty.Setsize(terminal, &pty.Winsize{Rows: 15, Cols: 42}); err != nil {
		t.Fatal(err)
	}
	write("\x03", 180*time.Millisecond)
	write("n", 100*time.Millisecond)
	write("\x03", 180*time.Millisecond)
	write("y", 100*time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("composer PTY helper failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("composer PTY helper did not exit; output:\n%s", captured.String())
	}

	time.Sleep(50 * time.Millisecond)
	output := captured.String()
	for _, want := range []string{"hello", "café", "save this draft for later?"} {
		if !strings.Contains(output, want) {
			t.Errorf("composer PTY output missing %q; output:\n%s", want, output)
		}
	}
	if strings.Contains(strings.ToLower(output), "panic") {
		t.Fatalf("composer PTY output contains panic:\n%s", output)
	}
}
