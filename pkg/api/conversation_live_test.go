package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

// Read-only smoke test for X's unsupported TweetDetail endpoint.
func TestTweetDetailLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_CONVERSATION") != "1" {
		t.Skip("set XEET_LIVE_CONVERSATION=1 to run")
	}
	mgr, err := config.NewConfigManager()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := NewWebClient(cfg)
	home, err := client.FetchHomeTimeline(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Posts) == 0 {
		t.Skip("home timeline returned no posts")
	}
	page, err := client.FetchTweetDetail(ctx, home.Posts[0].ID, "", 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) == 0 || page.Posts[0].ID != home.Posts[0].ID {
		t.Fatalf("focal post missing: selected=%s page=%+v", home.Posts[0].ID, page)
	}
	if page.Cursor != "" {
		if _, err := client.FetchTweetDetail(ctx, home.Posts[0].ID, page.Cursor, 40); err != nil {
			t.Fatalf("fetch reply continuation: %v", err)
		}
	}
}
