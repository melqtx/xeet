package api

import (
	"context"
	"os"
	"testing"
	"time"

	"xeet/pkg/config"
)

func TestHomeTimelineLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_TIMELINE") != "1" {
		t.Skip("set XEET_LIVE_TIMELINE=1 to run")
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
	page, err := NewWebClient(cfg).FetchHomeTimeline(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded %d posts; cursor=%t", len(page.Posts), page.Cursor != "")
}
