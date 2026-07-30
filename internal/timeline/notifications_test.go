package timeline

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/config"
)

func TestFetchPageSeqRoutesNotificationsFeedToNotificationsOperation(t *testing.T) {
	secrets := &fakeRequestSecretStore{data: map[string]string{}}
	manager := config.NewConfigManagerAt(t.TempDir(), secrets)
	if err := manager.Save(&config.Config{
		UserID: "42", Handle: "alice", AuthToken: "alice-auth", CT0: "alice-csrf",
		NotificationsTimelineQID: "notif-qid",
	}); err != nil {
		t.Fatal(err)
	}
	useRequestConfigManager(t, manager)

	originalTransport := http.DefaultTransport
	var path string
	http.DefaultTransport = requestRoundTripper(func(request *http.Request) (*http.Response, error) {
		path = request.URL.Path
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":{"entries":[]}}`)),
			Request:    request,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	msg := fetchPageSeq(
		context.Background(), FeedNotifications, "", "", "", "", "", false, 0, 0, false,
	)().(pageMsg)
	if msg.err != nil {
		t.Fatalf("notifications fetch: %v", msg.err)
	}
	if !strings.HasSuffix(path, "/NotificationsTimeline") {
		t.Fatalf("request path = %q, want the NotificationsTimeline operation", path)
	}
}

func TestNotificationsColumnLabelsAndSearchBackLabels(t *testing.T) {
	m := New()
	m.cur().feed = FeedNotifications
	m.cur().loading = false

	if status := m.columnFeedLabel(m.cur(), 40); status != "notifications" {
		t.Fatalf("column label = %q, want notifications", status)
	}
	if label := m.searchBackLabel(); label != "back to notifications" {
		t.Fatalf("search back label = %q", label)
	}
	if label := m.searchBackShortLabel(); label != "notifications" {
		t.Fatalf("search back short label = %q", label)
	}
}
