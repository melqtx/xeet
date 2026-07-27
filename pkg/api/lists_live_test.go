package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

func TestListsLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_LISTS") != "1" {
		t.Skip("set XEET_LIVE_LISTS=1 to run")
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

	lists, err := client.FetchOwnedLists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("lists: %d", len(lists))
	if len(lists) == 0 {
		t.Skip("the authenticated account owns or follows no lists")
	}
	if lists[0].ID == "" || lists[0].Name == "" {
		t.Fatalf("first list has incomplete identity: %+v", lists[0])
	}

	page, err := client.FetchListTimeline(ctx, lists[0].ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first list page: %d posts; cursor=%t", len(page.Posts), page.Cursor != "")

	if cfg.ListLatestTweetsTimelineQID == "" || cfg.ListsManagementPageTimelineQID == "" {
		if !client.ApplyRefreshedQueryIDs(cfg) {
			t.Fatal("reached the lists endpoints but staged no query ids for persistence")
		}
		if cfg.ListLatestTweetsTimelineQID == "" || cfg.ListsManagementPageTimelineQID == "" {
			t.Fatalf("ApplyRefreshedQueryIDs left a lists query id empty: %+v", cfg)
		}
	}
}
