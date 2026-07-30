package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

// Resolving the session's own handle keeps the live run read-only against
// other accounts, and UserTweets mixes in the user's reposts, so the check
// asserts a non-empty page rather than authorship.
func TestProfileLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_PROFILE") != "1" {
		t.Skip("set XEET_LIVE_PROFILE=1 to run")
	}
	mgr, err := config.NewConfigManager()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Handle == "" {
		t.Skip("config has no handle; run 'xeet auth' first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := NewWebClient(cfg)

	userID, err := client.FetchUserByScreenName(ctx, cfg.Handle)
	if err != nil {
		t.Fatalf("resolve @%s: %v", cfg.Handle, err)
	}
	if userID == "" {
		t.Fatal("lookup returned an empty user id")
	}
	t.Logf("@%s resolved to %s", cfg.Handle, userID)

	page, err := client.FetchUserTweets(ctx, userID, "", 20)
	if err != nil {
		t.Fatalf("fetch user tweets: %v", err)
	}
	if len(page.Posts) == 0 {
		t.Fatal("profile page came back empty")
	}
	t.Logf("got %d posts, cursor=%q", len(page.Posts), page.Cursor)

	if client.ApplyRefreshedQueryIDs(cfg) {
		if err := mgr.SaveQueryIDs(cfg); err != nil {
			t.Fatalf("persist refreshed query ids: %v", err)
		}
	}
}
