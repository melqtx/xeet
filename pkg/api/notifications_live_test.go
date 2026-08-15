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
	page, err := client.FetchNotifications(ctx, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if page.AccountID == "" {
		t.Fatal("notifications response did not identify the authenticated account")
	}
	t.Logf("notifications: %d actionable replies/mentions; cursor=%t", len(page.Notifications), page.Cursor != "")
	if cfg.NotificationsQID == "" && client.ApplyRefreshedQueryIDs(cfg) {
		t.Logf("discovered notifications query id staged for persistence (len=%d)", len(cfg.NotificationsQID))
	}
}
