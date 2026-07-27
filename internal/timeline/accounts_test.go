package timeline

import (
	"reflect"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeTimelineAccountStore struct {
	accounts []config.AccountInfo
	switched []string
}

func (f *fakeTimelineAccountStore) Accounts() ([]config.AccountInfo, error) {
	return append([]config.AccountInfo(nil), f.accounts...), nil
}

func (f *fakeTimelineAccountStore) SetActive(userID string) error {
	f.switched = append(f.switched, userID)
	for i := range f.accounts {
		f.accounts[i].Active = f.accounts[i].UserID == userID
	}
	return nil
}

func useFakeTimelineAccountStore(t *testing.T, store *fakeTimelineAccountStore) {
	t.Helper()
	previous := openTimelineAccountStore
	openTimelineAccountStore = func() (timelineAccountStore, error) { return store, nil }
	t.Cleanup(func() { openTimelineAccountStore = previous })
}

func TestAccountCycleResetsEveryColumnAndDropsInFlightPages(t *testing.T) {
	store := &fakeTimelineAccountStore{accounts: []config.AccountInfo{
		{UserID: "42", Handle: "alice", Active: true},
		{UserID: "84", Handle: "bob"},
	}}
	useFakeTimelineAccountStore(t, store)

	m := New()
	m.configureColumns([]ColumnSpec{
		{Kind: FeedForYou},
		{Kind: FeedFollowing},
	})
	m.accounts = append([]config.AccountInfo(nil), store.accounts...)
	oldMessages := make([]pageMsg, len(m.columns))
	oldSequences := make([]int, len(m.columns))
	for i := range m.columns {
		c := &m.columns[i]
		c.feedSeq = i + 3
		c.loading = false
		c.posts = []api.TimelinePost{{ID: "old"}}
		c.cursor = "cursor"
		oldSequences[i] = c.feedSeq
		oldMessages[i] = pageMsg{
			page:  &api.TimelinePage{Posts: []api.TimelinePost{{ID: "stale"}}},
			seq:   c.feedSeq,
			colID: c.id,
		}
	}

	model, cycle := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})
	if cycle == nil {
		t.Fatal("@ did not start an account cycle")
	}
	cycled := cycle()
	if _, ok := cycled.(accountCycledMsg); !ok {
		t.Fatalf("@ returned %T, want accountCycledMsg", cycled)
	}
	model, reload := model.(Model).Update(cycled)
	updated := model.(Model)
	if reload == nil {
		t.Fatal("account cycle did not schedule fresh pages")
	}
	if !reflect.DeepEqual(store.switched, []string{"84"}) {
		t.Fatalf("active account writes = %v", store.switched)
	}
	for i := range updated.columns {
		c := &updated.columns[i]
		if c.feedSeq != oldSequences[i]+1 || len(c.posts) != 0 || c.cursor != "" || !c.loading {
			t.Fatalf("column %d was not fully reset: %+v", i, c)
		}
	}

	for _, stale := range oldMessages {
		next, _ := updated.Update(stale)
		updated = next.(Model)
	}
	for i := range updated.columns {
		if len(updated.columns[i].posts) != 0 {
			t.Fatalf("stale page landed in column %d: %+v", i, updated.columns[i].posts)
		}
	}
	if got := updated.activeAccountLabel(); got != "@bob" {
		t.Fatalf("active account label = %q, want @bob", got)
	}
}

func TestActiveAccountHandleAppearsOnlyWhenSeveralAccountsAreSaved(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.accounts = []config.AccountInfo{
		{UserID: "42", Handle: "alice", Active: true},
		{UserID: "84", Handle: "bob"},
	}
	if header := m.header(m.contentWidth()); !strings.Contains(header, "@alice") {
		t.Fatalf("multi-account header omitted the active handle:\n%s", header)
	}

	m.accounts = m.accounts[:1]
	if header := m.header(m.contentWidth()); strings.Contains(header, "@alice") {
		t.Fatalf("single-account header added redundant identity:\n%s", header)
	}
}

func TestTimelineHelpKeepsAltTextOnAAndUsesAtForAccounts(t *testing.T) {
	m := New()
	m.width = 80
	m.height = 30
	view := m.viewHelp()
	for _, want := range []string{"@           next account", "A           image alt text"} {
		if !strings.Contains(view, want) {
			t.Fatalf("help omitted %q:\n%s", want, view)
		}
	}
}
