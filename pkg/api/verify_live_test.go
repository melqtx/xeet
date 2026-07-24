package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

// TestVerifyLive confirms the saved browser session against X without making
// any account mutation. Run explicitly with XEET_LIVE_VERIFY=1.
func TestVerifyLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_VERIFY") != "1" {
		t.Skip("set XEET_LIVE_VERIFY=1 to run")
	}
	manager, err := config.NewConfigManager()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if _, err := NewWebClient(cfg).Verify(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("session verification succeeded")
}
