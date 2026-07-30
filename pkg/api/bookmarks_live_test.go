package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

func TestBookmarksLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_BOOKMARKS") != "1" {
		t.Skip("set XEET_LIVE_BOOKMARKS=1 to run")
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
	first, err := client.FetchBookmarks(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first page: %d posts; cursor=%t", len(first.Posts), first.Cursor != "")
	if len(first.Posts) == 0 {
		t.Fatal("bookmarks returned no posts; the account has bookmarks, so an empty page means the variables or the parser are wrong")
	}
	if first.Cursor == "" {
		t.Fatal("no bottom cursor; pagination would dead-end after the first page")
	}

	second, err := client.FetchBookmarks(ctx, first.Cursor, 10)
	if err != nil {
		t.Fatalf("second page with cursor %q: %v", first.Cursor, err)
	}
	t.Logf("second page: %d posts", len(second.Posts))
	if len(second.Posts) > 0 && second.Posts[0].ID == first.Posts[0].ID {
		t.Fatal("cursor did not advance; the second page repeats the first")
	}

	// A discovered id that never reaches the config is not an error the caller
	// can see: it just silently re-discovers on every launch.
	if cfg.BookmarksQID == "" {
		if !client.ApplyRefreshedQueryIDs(cfg) {
			t.Fatal("discovered a query id to reach the endpoint but nothing was staged for persistence")
		}
		if cfg.BookmarksQID == "" {
			t.Fatal("ApplyRefreshedQueryIDs reported work but left BookmarksQID empty")
		}
		t.Logf("discovered bookmarks query id staged for persistence (len=%d)", len(cfg.BookmarksQID))
	}
}

// The bookmark round trip cannot be proven by mocks: only a live
// Create/Delete pair against a known post shows both the mutation shape and
// the legacy.bookmarked flag flipping back.
func TestBookmarkMutationLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_BOOKMARK") != "1" {
		t.Skip("set XEET_LIVE_BOOKMARK=1 to run")
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

	tweetID := os.Getenv("XEET_LIVE_BOOKMARK_TWEET_ID")
	if tweetID == "" {
		// Hands-free default: bookmark one of the account's own posts.
		if cfg.Handle == "" {
			t.Skip("set XEET_LIVE_BOOKMARK_TWEET_ID to one of your own older posts")
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
			t.Skip("no own posts found to bookmark; set XEET_LIVE_BOOKMARK_TWEET_ID")
		}
		t.Logf("bookmark target: own post %s", tweetID)
	}

	bookmarked := func(want bool) {
		t.Helper()
		page, err := client.FetchTweetDetail(ctx, tweetID, "", 5)
		if err != nil {
			t.Fatalf("read back tweet detail: %v", err)
		}
		for _, post := range page.Posts {
			if post.ID == tweetID {
				if post.Bookmarked != want {
					t.Fatalf("bookmarked flag = %v, want %v", post.Bookmarked, want)
				}
				return
			}
		}
		t.Fatalf("tweet %s missing from its own detail page", tweetID)
	}

	if err := client.SetTweetBookmarked(ctx, tweetID, true); err != nil {
		t.Fatalf("CreateBookmark: %v", err)
	}
	// Even a failed verify below must attempt to undo the bookmark; leave
	// nothing behind on the account.
	t.Cleanup(func() {
		uctx, ucancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ucancel()
		if err := client.SetTweetBookmarked(uctx, tweetID, false); err != nil {
			t.Logf("cleanup un-bookmark failed (remove it by hand): %v", err)
		}
	})
	bookmarked(true)

	if err := client.SetTweetBookmarked(ctx, tweetID, false); err != nil {
		t.Fatalf("DeleteBookmark: %v", err)
	}
	bookmarked(false)

	for operation, qid := range map[string]string{
		"CreateBookmark": cfg.CreateBookmarkQID,
		"DeleteBookmark": cfg.DeleteBookmarkQID,
	} {
		if qid != "" {
			continue
		}
		if !client.ApplyRefreshedQueryIDs(cfg) {
			t.Fatalf("discovered a %s id but nothing was staged for persistence", operation)
		}
		t.Logf("discovered %s query id staged for persistence", operation)
	}
	if cfg.CreateBookmarkQID == "" || cfg.DeleteBookmarkQID == "" {
		t.Fatalf("staged ids incomplete: create=%q delete=%q", cfg.CreateBookmarkQID, cfg.DeleteBookmarkQID)
	}
	if err := mgr.SaveQueryIDs(cfg); err != nil {
		t.Fatalf("persist refreshed query ids: %v", err)
	}
}
