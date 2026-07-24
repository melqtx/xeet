//go:build darwin || linux

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/melqtx/xeet/internal/theme"

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

func TestThemePickerPTYHelper(t *testing.T) {
	if os.Getenv("XEET_THEME_PTY_HELPER") != "1" {
		return
	}
	chosen, err := runThemePicker(context.Background(), theme.Names()[0])
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("CHOSE:" + chosen)
}

// TestThemePickerPTYRunsInARealTerminal drives the picker the way a person
// does: through a terminal, one keystroke at a time. It catches the failures
// unit tests on Update cannot, such as a program that never renders or never
// exits.
func TestThemePickerPTYRunsInARealTerminal(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestThemePickerPTYHelper$")
	cmd.Env = append(os.Environ(), "XEET_THEME_PTY_HELPER=1", "TERM=xterm-256color", "NO_COLOR=1")
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 100})
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

	// Answer the terminal queries lipgloss makes on startup first. A pty with
	// nobody on the other end never replies, and the reader waiting on that
	// reply would otherwise swallow the keystrokes below.
	time.Sleep(150 * time.Millisecond)
	write("\x1b]11;rgb:0000/0000/0000\x1b\\\x1b[1;1R", 100*time.Millisecond)
	write("j", 150*time.Millisecond)
	write("\r", 150*time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("theme picker PTY helper failed: %v\noutput:\n%s", err, captured.String())
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("theme picker PTY helper did not exit; output:\n%s", captured.String())
	}

	time.Sleep(50 * time.Millisecond)
	output := captured.String()
	for _, want := range []string{"pick a theme", "enter to xeet", "CHOSE:" + theme.Names()[1]} {
		if !strings.Contains(output, want) {
			t.Errorf("theme picker PTY output missing %q; output:\n%s", want, output)
		}
	}
	if strings.Contains(strings.ToLower(output), "panic") {
		t.Fatalf("theme picker PTY output contains panic:\n%s", output)
	}
}
