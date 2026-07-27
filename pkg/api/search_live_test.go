package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

func TestSearchTimelineLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_SEARCH") != "1" {
		t.Skip("set XEET_LIVE_SEARCH=1 to run")
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
	first, err := client.FetchSearchTimeline(ctx, "golang", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first page: %d posts; cursor=%t", len(first.Posts), first.Cursor != "")
	if len(first.Posts) == 0 {
		t.Fatal("search returned no posts; a popular query returning nothing means the variables or the entry walker are wrong")
	}
	if first.Cursor == "" {
		t.Fatal("no bottom cursor; pagination would dead-end after the first page")
	}

	second, err := client.FetchSearchTimeline(ctx, "golang", first.Cursor, 10)
	if err != nil {
		t.Fatalf("second page with cursor %q: %v", first.Cursor, err)
	}
	t.Logf("second page: %d posts", len(second.Posts))
	if len(second.Posts) > 0 && second.Posts[0].ID == first.Posts[0].ID {
		t.Fatal("cursor did not advance; the second page repeats the first")
	}

	// operationHint used to route anything containing "Tweet" to the composer
	// bundle, which silently starved SearchTimeline of a query id.
	if cfg.SearchTimelineQID == "" {
		if !client.ApplyRefreshedQueryIDs(cfg) {
			t.Fatal("reached the endpoint but staged no query id for persistence")
		}
		if cfg.SearchTimelineQID == "" {
			t.Fatal("ApplyRefreshedQueryIDs reported work but left SearchTimelineQID empty")
		}
		t.Logf("discovered search query id staged for persistence (len=%d)", len(cfg.SearchTimelineQID))
	}
}
