//go:build darwin || linux

package timeline

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/api"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
)

// ptyModel suppresses the production network-loading Init command while
// preserving the real timeline Update and View behavior.
type synchronizedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type ptyModel struct{ Model }

func (m ptyModel) Init() tea.Cmd { return nil }

func (m ptyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.Model.Update(msg)
	return ptyModel{Model: next.(Model)}, cmd
}

func (m ptyModel) View() string { return m.Model.View() }

func TestTimelinePTYHelper(t *testing.T) {
	if os.Getenv("XEET_PTY_HELPER") != "1" {
		return
	}
	m := New()
	m.configureColumns(repeatedColumnSpecs(2, FeedForYou, "", ""))
	m.columns[0].loading = false
	m.columns[0].posts = []api.TimelinePost{
		{ID: "1", AuthorName: "Alice", Handle: "alice", Text: "hello from the PTY", LikeCount: 3},
		{ID: "2", AuthorName: "Bob", Handle: "bob", Text: "unicode: 🐈 café 日本語", MediaCount: 1},
	}
	m.columns[1].loading = false
	m.columns[1].posts = []api.TimelinePost{
		{ID: "3", AuthorName: "Carol", Handle: "carol", Text: "second PTY column"},
	}
	m.resize()
	if _, err := tea.NewProgram(ptyModel{Model: m}, tea.WithAltScreen(), tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run(); err != nil {
		t.Fatal(err)
	}
}

func TestTimelinePTYNavigationResizeAndHelp(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestTimelinePTYHelper$")
	// Bubble Tea checks the terminal background during package initialization,
	// before TestMain can establish the helper's isolated terminal environment.
	// Start with TERM=dumb to skip that interactive probe; TestMain restores
	// xterm-256color before the helper test itself runs.
	cmd.Env = append(os.Environ(), "XEET_PTY_HELPER=1", "TERM=dumb", "NO_COLOR=1")
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()

	var captured synchronizedBuffer
	go func() { _, _ = io.Copy(&captured, terminal) }()

	writeKey := func(key string) {
		t.Helper()
		if _, err := terminal.WriteString(key); err != nil {
			t.Fatal(err)
		}
		time.Sleep(80 * time.Millisecond)
	}

	time.Sleep(120 * time.Millisecond)
	writeKey("j")
	writeKey("/")
	writeKey("terminal cats")
	writeKey("\x1b")
	if err := pty.Setsize(terminal, &pty.Winsize{Rows: 15, Cols: 42}); err != nil {
		t.Fatal(err)
	}
	writeKey("?")
	time.Sleep(200 * time.Millisecond)
	writeKey("?")
	writeKey("q\n")

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PTY helper failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("PTY helper did not exit after q; output:\n%s", captured.String())
	}

	time.Sleep(50 * time.Millisecond)
	output := captured.String()
	for _, want := range []string{"xeet", "Alice", "search posts", "timeline keys", "+1 more (widen terminal)"} {
		if !strings.Contains(output, want) {
			t.Errorf("PTY output missing %q; output:\n%s", want, output)
		}
	}
	if strings.Contains(strings.ToLower(output), "panic") {
		t.Fatalf("PTY output contains panic:\n%s", output)
	}
}
