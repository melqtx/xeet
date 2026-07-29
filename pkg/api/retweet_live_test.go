package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

// The repost round trip cannot be proven by mocks: the DeleteRetweet variable
// name is a hypothesis until the real endpoint accepts it, and only a live
// Create/Delete pair against a known post shows both the mutation shape and
// the legacy.retweeted flag flipping back.
func TestRepostLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_REPOST") != "1" {
		t.Skip("set XEET_LIVE_REPOST=1 to run")
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

	tweetID := os.Getenv("XEET_LIVE_REPOST_TWEET_ID")
	if tweetID == "" {
		// Hands-free default: repost one of the account's own posts so nobody
		// else gets a notification from the test.
		if cfg.Handle == "" {
			t.Skip("set XEET_LIVE_REPOST_TWEET_ID to one of your own older posts")
		}
		found, err := client.FetchSearchTimeline(ctx, "from:"+cfg.Handle, "", 10)
		if err != nil {
			t.Fatalf("search own posts: %v", err)
		}
		for _, post := range found.Posts {
			if post.Handle == cfg.Handle {
				tweetID = post.ID
				break
			}
		}
		if tweetID == "" {
			t.Skip("no own posts found to repost; set XEET_LIVE_REPOST_TWEET_ID")
		}
		t.Logf("repost target: own post %s", tweetID)
	}

	reposted := func(want bool) {
		t.Helper()
		page, err := client.FetchTweetDetail(ctx, tweetID, "", 5)
		if err != nil {
			t.Fatalf("read back tweet detail: %v", err)
		}
		for _, post := range page.Posts {
			if post.ID == tweetID {
				if post.Reposted != want {
					t.Fatalf("retweeted flag = %v, want %v", post.Reposted, want)
				}
				return
			}
		}
		t.Fatalf("tweet %s missing from its own detail page", tweetID)
	}

	if err := client.SetTweetReposted(ctx, tweetID, true); err != nil {
		t.Fatalf("CreateRetweet: %v", err)
	}
	// Even a failed verify below must attempt to undo the repost; leave nothing
	// behind on the account.
	t.Cleanup(func() {
		uctx, ucancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ucancel()
		if err := client.SetTweetReposted(uctx, tweetID, false); err != nil {
			t.Logf("cleanup unrepost failed (remove it by hand): %v", err)
		}
	})
	reposted(true)

	if err := client.SetTweetReposted(ctx, tweetID, false); err != nil {
		t.Fatalf("DeleteRetweet: %v", err)
	}
	reposted(false)

	if cfg.CreateRetweetQID == "" {
		if !client.ApplyRefreshedQueryIDs(cfg) || cfg.CreateRetweetQID == "" {
			t.Fatal("discovered a CreateRetweet id but nothing was staged for persistence")
		}
		t.Logf("discovered CreateRetweet query id staged for persistence (len=%d)", len(cfg.CreateRetweetQID))
	}
	if cfg.DeleteRetweetQID == "" {
		if !client.ApplyRefreshedQueryIDs(cfg) || cfg.DeleteRetweetQID == "" {
			t.Fatal("discovered a DeleteRetweet id but nothing was staged for persistence")
		}
		t.Logf("discovered DeleteRetweet query id staged for persistence (len=%d)", len(cfg.DeleteRetweetQID))
	}
}
