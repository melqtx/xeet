package timeline

import (
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/api"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestViewColumnsDrawsSeparator(t *testing.T) {
	m := modelWithNamedColumns("Alice", "Bob")
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})

	view := ansi.Strip(m.View())
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "for you") && !strings.Contains(line, "│") {
			t.Fatalf("the header row lost its column separator: %q\n%s", line, view)
		}
	}
}

func TestColumnHeadersAlignOnOneRow(t *testing.T) {
	m := modelWithNamedColumns("Alice", "Bob", "Carol")
	m = update(t, m, tea.WindowSizeMsg{Width: 160, Height: 24})

	view := ansi.Strip(m.View())
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "for you") {
			if got := strings.Count(line, "for you"); got != 3 {
				t.Fatalf("headers spread across rows, one row holds %d of 3:\n%s", got, view)
			}
			return
		}
	}
	t.Fatalf("no header row found:\n%s", view)
}

func TestColumnHeaderShowsAccountWhenMultiple(t *testing.T) {
	m := modelWithNamedColumns("Alice", "Bob")
	m.accounts = twoAccounts()
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "@alice · for you") {
		t.Fatalf("multi-account headers did not name their account:\n%s", view)
	}
}

func TestColumnHeaderHidesAccountWhenSingle(t *testing.T) {
	m := modelWithNamedColumns("Alice", "Bob")
	m.accounts = twoAccounts()[:1]
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})

	view := ansi.Strip(m.View())
	if strings.Contains(view, "@alice ·") {
		t.Fatalf("single-account headers wasted space on the account name:\n%s", view)
	}
	if !strings.Contains(view, "▸ for you") {
		t.Fatalf("single-account header lost its feed label:\n%s", view)
	}
}

func TestPostTextKeepsLineBreaks(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.cur().posts = []api.TimelinePost{{
		ID: "1", AuthorName: "Alice", Handle: "alice",
		Text: "first  line\nsecond line\n\nthird line",
	}}
	m.syncViewport()

	content, _, _ := m.renderFeedContent()
	lines := strings.Split(ansi.Strip(content), "\n")
	at := -1
	for i, line := range lines {
		if strings.Contains(line, "first line") {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("post body missing:\n%s", content)
	}
	if !strings.Contains(lines[at+1], "second line") {
		t.Fatalf("the second line did not stay on its own row:\n%s", content)
	}
	if !strings.Contains(lines[at+2], "third line") {
		t.Fatalf("the blank line was not dropped before the third line:\n%s", content)
	}
	if got := cleanText("a  b\r\n\nc"); got != "a b\nc" {
		t.Fatalf("cleanText = %q, want whitespace collapsed per line with blanks dropped", got)
	}
}

func TestSelectedPostUsesHeavyMarker(t *testing.T) {
	m := NewWithImageMode("off")
	m.cur().loading = false
	m.cur().posts = []api.TimelinePost{
		{ID: "1", AuthorName: "Alice", Handle: "alice", Text: "chosen"},
		{ID: "2", AuthorName: "Bob", Handle: "bob", Text: "not chosen"},
	}
	m.cur().selected = 0
	m.syncViewport()

	content, _, _ := m.renderFeedContent()
	for _, line := range strings.Split(ansi.Strip(content), "\n") {
		if strings.Contains(line, "Alice") && !strings.Contains(line, "▌") {
			t.Fatalf("the selected post lost its heavy marker: %q\n%s", line, content)
		}
	}
}
