package timeline

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFetchPageSeqRoutesProfileFeedToUserTweetsOperation(t *testing.T) {
	secrets := &fakeRequestSecretStore{data: map[string]string{}}
	manager := config.NewConfigManagerAt(t.TempDir(), secrets)
	if err := manager.Save(&config.Config{
		UserID: "42", Handle: "alice", AuthToken: "alice-auth", CT0: "alice-csrf",
		UserTweetsQID: "ut-qid",
	}); err != nil {
		t.Fatal(err)
	}
	useRequestConfigManager(t, manager)

	originalTransport := http.DefaultTransport
	var path, userID string
	http.DefaultTransport = requestRoundTripper(func(request *http.Request) (*http.Response, error) {
		path = request.URL.Path
		userID = request.URL.Query().Get("variables")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":{"entries":[]}}`)),
			Request:    request,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	msg := fetchPageSeq(
		context.Background(), FeedProfile, "", "", "999", "42", "", false, 0, 0, false,
	)().(pageMsg)
	if msg.err != nil {
		t.Fatalf("profile fetch: %v", msg.err)
	}
	if !strings.HasSuffix(path, "/UserTweets") {
		t.Fatalf("request path = %q, want the UserTweets operation", path)
	}
	if !strings.Contains(userID, `"userId":"999"`) {
		t.Fatalf("variables = %q, want the resolved user id", userID)
	}
}

func TestProfileKeySwitchesFocusedColumn(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().posts = posts(3)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = next.(Model)
	c := m.cur()
	if cmd == nil {
		t.Fatal("u produced no resolve request")
	}
	if c.feed != FeedProfile || c.profileHandle != "cat" || c.profileUserID != "" {
		t.Fatalf("column after u: feed=%v handle=%q uid=%q", c.feed, c.profileHandle, c.profileUserID)
	}
	if !c.loading || len(c.posts) != 0 {
		t.Fatalf("column was not reset for the profile: loading=%v posts=%d", c.loading, len(c.posts))
	}
}

func TestProfileResolveSuccessStoresIDAndFetches(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().posts = posts(3)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = next.(Model)

	cmd := m.applyProfileResult(profileMsg{colID: m.cur().id, handle: "cat", userID: "999"})
	if cmd == nil {
		t.Fatal("successful resolve kicked no first-page fetch")
	}
	if m.cur().profileUserID != "999" {
		t.Fatalf("profileUserID = %q, want 999", m.cur().profileUserID)
	}
}

func TestProfileResolveFailureSurfacesError(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.cur().posts = posts(3)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = next.(Model)

	m.applyProfileResult(profileMsg{colID: m.cur().id, handle: "cat", err: errors.New("boom")})
	c := m.cur()
	if c.err == nil || c.loading {
		t.Fatalf("failed resolve: err=%v loading=%v", c.err, c.loading)
	}
	if !strings.Contains(m.toast, "couldn't load @cat") {
		t.Fatalf("toast = %q", m.toast)
	}
}

func TestProfileKeyFromThreadCollapsesToFeed(t *testing.T) {
	m := New()
	m.cur().loading = false
	m.mode = modeThread
	m.cur().threadPosts = []api.ConversationPost{{TimelinePost: api.TimelinePost{ID: "1", Text: "post", Handle: "alice"}}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("u in thread produced no resolve request")
	}
	if m.mode != modeFeed || m.cur().feed != FeedProfile || m.cur().profileHandle != "alice" {
		t.Fatalf("mode=%v feed=%v handle=%q", m.mode, m.cur().feed, m.cur().profileHandle)
	}
}

func TestProfileColumnLabels(t *testing.T) {
	m := New()
	m.cur().feed = FeedProfile
	m.cur().profileHandle = "cat"
	m.cur().loading = false

	if status := m.columnFeedLabel(m.cur(), 40); status != "@cat" {
		t.Fatalf("column label = %q, want @cat", status)
	}
	if label := m.searchBackLabel(); label != "back to @cat" {
		t.Fatalf("search back label = %q", label)
	}
	if label := m.searchBackShortLabel(); label != "@cat" {
		t.Fatalf("search back short label = %q", label)
	}
}
