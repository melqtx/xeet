package api

import (
	"context"
	"os"
	"testing"
	"time"
)

// Gated live test: hits x.com + abs.twimg.com to confirm discovery finds a real
// CreateTweet queryId from the current public JS bundles. Run with
// XEET_LIVE_DISCOVER=1.
func TestDiscoverLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_DISCOVER") != "1" {
		t.Skip("set XEET_LIVE_DISCOVER=1 to run the live discovery test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	id, err := DiscoverCreateTweetQueryID(ctx, "", "")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}
	t.Logf("discovered CreateTweet queryId: %s", id)
	if len(id) < 8 {
		t.Fatalf("suspicious queryId: %q", id)
	}
}
