package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

func TestNotificationsLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_NOTIFICATIONS") != "1" {
		t.Skip("set XEET_LIVE_NOTIFICATIONS=1 to run")
	}
	mgr, err := config.NewConfigManager()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	client := NewWebClient(cfg)
	first, err := client.FetchNotificationsTimeline(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first page: %d notifications; cursor=%t", len(first.Posts), first.Cursor != "")
	if len(first.Posts) == 0 {
		t.Fatal("notifications returned no posts; the account has notifications, so an empty page means the variables or the parser are wrong")
	}
	for i, post := range first.Posts {
		t.Logf("  [%d] id=%s handle=%q text=%q", i, post.ID, post.Handle, post.Text)
	}
	if first.Cursor == "" {
		t.Fatal("no bottom cursor; pagination would dead-end after the first page")
	}

	second, err := client.FetchNotificationsTimeline(ctx, first.Cursor, 10)
	if err != nil {
		t.Fatalf("second page with cursor %q: %v", first.Cursor, err)
	}
	t.Logf("second page: %d notifications", len(second.Posts))
	if len(second.Posts) > 0 && second.Posts[0].ID == first.Posts[0].ID {
		t.Fatal("cursor did not advance; the second page repeats the first")
	}

	// A discovered id that never reaches the config is not an error the caller
	// can see: it just silently re-discovers on every launch.
	if cfg.NotificationsTimelineQID == "" {
		if !client.ApplyRefreshedQueryIDs(cfg) {
			t.Fatal("discovered a query id to reach the endpoint but nothing was staged for persistence")
		}
		if cfg.NotificationsTimelineQID == "" {
			t.Fatal("ApplyRefreshedQueryIDs reported work but left NotificationsTimelineQID empty")
		}
		t.Logf("discovered notifications query id staged for persistence (len=%d)", len(cfg.NotificationsTimelineQID))
	}
}
