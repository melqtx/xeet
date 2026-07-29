package timeline

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeRequestSecretStore struct {
	data map[string]string
}

func (f *fakeRequestSecretStore) Get(key string) (string, error) {
	value, ok := f.data[key]
	if !ok {
		return "", config.ErrSecretNotFound
	}
	return value, nil
}

func (f *fakeRequestSecretStore) Set(key, value string) error {
	f.data[key] = value
	return nil
}

func (f *fakeRequestSecretStore) Delete(key string) error {
	delete(f.data, key)
	return nil
}

type requestRoundTripper func(*http.Request) (*http.Response, error)

func (f requestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

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

func useRequestConfigManager(t *testing.T, manager requestConfigManager) {
	t.Helper()
	previous := openRequestConfigManager
	openRequestConfigManager = func() (requestConfigManager, error) { return manager, nil }
	t.Cleanup(func() { openRequestConfigManager = previous })
}

func TestColumnFetchUsesItsOwnAccountSession(t *testing.T) {
	secrets := &fakeRequestSecretStore{data: map[string]string{}}
	manager := config.NewConfigManagerAt(t.TempDir(), secrets)
	if err := manager.Save(&config.Config{
		UserID: "42", Handle: "alice", AuthToken: "alice-auth", CT0: "alice-csrf",
		HomeTimelineQID: "home-qid",
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveAccount(&config.Config{
		UserID: "84", Handle: "bob", AuthToken: "bob-auth", CT0: "bob-csrf",
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetActive("42"); err != nil {
		t.Fatal(err)
	}
	useRequestConfigManager(t, manager)

	originalTransport := http.DefaultTransport
	var cookies []string
	var csrfTokens []string
	http.DefaultTransport = requestRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "x.com" {
			t.Fatalf("request host = %q, want x.com", request.URL.Host)
		}
		cookies = append(cookies, request.Header.Get("Cookie"))
		csrfTokens = append(csrfTokens, request.Header.Get("X-Csrf-Token"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":{"entries":[]}}`)),
			Request:    request,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	for index, accountID := range []string{"42", "84"} {
		msg := fetchPageSeq(
			context.Background(), FeedForYou, "", "", accountID, "", false, index, index, false,
		)().(pageMsg)
		if msg.err != nil {
			t.Fatalf("account %s fetch: %v", accountID, msg.err)
		}
	}

	wantCookies := []string{
		"auth_token=alice-auth; ct0=alice-csrf",
		"auth_token=bob-auth; ct0=bob-csrf",
	}
	if !reflect.DeepEqual(cookies, wantCookies) {
		t.Fatalf("request Cookie headers = %q, want %q", cookies, wantCookies)
	}
	if !reflect.DeepEqual(csrfTokens, []string{"alice-csrf", "bob-csrf"}) {
		t.Fatalf("request X-Csrf-Token headers = %q", csrfTokens)
	}
}

func TestAccountSwitchMidFlightDropsStalePageViaSeqGuard(t *testing.T) {
	m := New()
	m.configureColumns([]ColumnSpec{{Kind: FeedForYou, AccountID: "42"}})
	c := m.cur()
	c.feedSeq = 7
	stale := pageMsg{
		page:  &api.TimelinePage{Posts: []api.TimelinePost{{ID: "old-account"}}},
		seq:   c.feedSeq,
		colID: c.id,
	}

	c.accountID = "84"
	m.setFeed(c.feed)
	next, _ := m.applyFeedPage(stale)
	m = next.(Model)

	if m.cur().accountID != "84" || m.cur().feedSeq != 8 {
		t.Fatalf("column account switch did not bump the feed sequence: %+v", m.cur())
	}
	if len(m.cur().posts) != 0 {
		t.Fatalf("old-account page landed after the switch: %+v", m.cur().posts)
	}
}

func TestLikeFanOutStaysWithinTheActingAccount(t *testing.T) {
	m := New()
	m.configureColumns([]ColumnSpec{
		{Kind: FeedForYou, AccountID: "42"},
		{Kind: FeedForYou, AccountID: "84"},
		{Kind: FeedFollowing, AccountID: "42"},
	})
	for i := range m.columns {
		m.columns[i].posts = []api.TimelinePost{{ID: "shared"}}
		m.columns[i].threadPosts = []api.ConversationPost{{
			TimelinePost: api.TimelinePost{ID: "shared"},
		}}
	}

	if cmd := m.toggleSelectedLike(); cmd == nil {
		t.Fatal("account 42 like did not start")
	}
	if !m.liking[likeKey("42", "shared")] || m.liking[likeKey("84", "shared")] {
		t.Fatalf("in-flight likes are not account-keyed: %v", m.liking)
	}

	for _, index := range []int{0, 2} {
		if !m.columns[index].posts[0].Liked || !m.columns[index].threadPosts[0].Liked {
			t.Fatalf("account 42 column %d did not receive the like", index)
		}
	}
	if m.columns[1].posts[0].Liked || m.columns[1].threadPosts[0].Liked {
		t.Fatal("account 42 like crossed into account 84 state")
	}
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
