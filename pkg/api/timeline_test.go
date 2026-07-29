package api

import (
	"encoding/json"
	"testing"
)

func TestParseTimeline(t *testing.T) {
	fixture := `{"entries":[
	  {"itemContent":{"tweet_results":{"result":{
	    "rest_id":"1",
	    "legacy":{"full_text":"hello timeline","reply_count":2,"retweet_count":3,"favorite_count":4,"favorited":true,"retweeted":true,"created_at":"Mon Jul 20 10:00:00 +0000 2026","extended_entities":{"media":[{"type":"photo","media_url_https":"https://pbs.twimg.com/media/abc","ext_alt_text":"a cat","original_info":{"width":1200,"height":800}}]}},
	    "views":{"count":"55"},
	    "core":{"user_results":{"result":{"is_blue_verified":true,"core":{"name":"Alice","screen_name":"alice"}}}}
	  }}}},
	  {"itemContent":{"tweet_results":{"result":{"tweet":{
	    "rest_id":"2","legacy":{"full_text":"short"},
	    "note_tweet":{"note_tweet_results":{"result":{"text":"this is the long note"}}},
	    "core":{"user_results":{"result":{"legacy":{"name":"Bob","screen_name":"bob"}}}}
	  }}}}},
	  {"cursorType":"Bottom","value":"next-page"}
	]}`
	var payload any
	if err := json.Unmarshal([]byte(fixture), &payload); err != nil {
		t.Fatal(err)
	}
	page := parseTimeline(payload)
	if len(page.Posts) != 2 || page.Cursor != "next-page" {
		t.Fatalf("unexpected page: %+v", page)
	}
	first := page.Posts[0]
	if first.Handle != "alice" || first.AuthorName != "Alice" || first.LikeCount != 4 || first.MediaCount != 1 || !first.Liked {
		t.Fatalf("unexpected first post: %+v", first)
	}
	if !first.Reposted || first.RepostCount != 3 {
		t.Fatalf("repost state lost: %+v", first)
	}
	if len(first.Media) != 1 || first.Media[0].URL != "https://pbs.twimg.com/media/abc" ||
		first.Media[0].AltText != "a cat" || first.Media[0].Width != 1200 || first.Media[0].Height != 800 {
		t.Fatalf("unexpected media: %+v", first.Media)
	}
	if page.Posts[1].Text != "this is the long note" || page.Posts[1].Handle != "bob" {
		t.Fatalf("unexpected wrapped post: %+v", page.Posts[1])
	}
}

func TestParseTimelineUnescapesText(t *testing.T) {
	item := map[string]any{"tweet_results": map[string]any{"result": map[string]any{
		"rest_id": "1", "legacy": map[string]any{"full_text": "&gt; hello &amp; goodbye"},
	}}}
	post, ok := parseTimelineItem(item)
	if !ok || post.Text != "> hello & goodbye" {
		t.Fatalf("text=%q ok=%v", post.Text, ok)
	}
}

func TestParseTimelineDeduplicatesTweet(t *testing.T) {
	item := map[string]any{"itemContent": map[string]any{"tweet_results": map[string]any{"result": map[string]any{"rest_id": "1", "legacy": map[string]any{"full_text": "same"}}}}}
	page := parseTimeline(map[string]any{"entries": []any{item, item}})
	if len(page.Posts) != 1 {
		t.Fatalf("got %d posts", len(page.Posts))
	}
}

func TestParseTimelineSkipsDeletedAndPromotedPosts(t *testing.T) {
	fixture := `{"data":{"timeline":{"entries":[
	  {"itemContent":{"tweet_results":{"result":{"__typename":"TweetTombstone","tombstone":{"text":{"text":"deleted"}}}}}},
	  {"itemContent":{"promoted_metadata":{},"tweet_results":{"result":{"rest_id":"ad","legacy":{"full_text":"ad"}}}}},
	  {"itemContent":{"tweet_results":{"result":{"rest_id":"ok","legacy":{"full_text":"real post"}}}}}
	]}}}`
	var payload any
	if err := json.Unmarshal([]byte(fixture), &payload); err != nil {
		t.Fatal(err)
	}
	page := parseTimeline(payload)
	if len(page.Posts) != 1 || page.Posts[0].ID != "ok" {
		t.Fatalf("posts = %+v", page.Posts)
	}
}

func TestParseTimelineVisibilityWrapper(t *testing.T) {
	fixture := `{"entries":[{"itemContent":{"tweet_results":{"result":{"__typename":"TweetWithVisibilityResults","tweet":{
	  "rest_id":"wrapped","legacy":{"full_text":"visible"},
	  "core":{"user_results":{"result":{"core":{"name":"Alice","screen_name":"alice"}}}}
	}}}}}]}`
	var payload any
	if err := json.Unmarshal([]byte(fixture), &payload); err != nil {
		t.Fatal(err)
	}
	page := parseTimeline(payload)
	if len(page.Posts) != 1 || page.Posts[0].ID != "wrapped" || page.Posts[0].Handle != "alice" {
		t.Fatalf("posts = %+v", page.Posts)
	}
}

func TestParseTimelineMultipleImages(t *testing.T) {
	fixture := `{"entries":[{"itemContent":{"tweet_results":{"result":{
	  "rest_id":"photos","legacy":{"full_text":"album","extended_entities":{"media":[
	    {"type":"photo","media_url_https":"https://pbs.twimg.com/media/a?format=jpg&name=small"},
	    {"type":"photo","media_url_https":"https://pbs.twimg.com/media/b?format=png&name=small","ext_alt_text":"second image"},
	    {"type":"photo"}
	  ]}}
	}}}}]}`
	var payload any
	if err := json.Unmarshal([]byte(fixture), &payload); err != nil {
		t.Fatal(err)
	}
	page := parseTimeline(payload)
	if len(page.Posts) != 1 || page.Posts[0].MediaCount != 2 || len(page.Posts[0].Media) != 2 {
		t.Fatalf("posts = %+v", page.Posts)
	}
	if page.Posts[0].Media[1].AltText != "second image" {
		t.Fatalf("media = %+v", page.Posts[0].Media)
	}
}

func TestParseTimelinePreservesDirectAndModuleItemOrderAndShowMoreCursor(t *testing.T) {
	item := func(id string) map[string]any {
		return map[string]any{"tweet_results": map[string]any{"result": map[string]any{
			"rest_id": id,
			"legacy":  map[string]any{"full_text": id},
		}}}
	}
	payload := map[string]any{"data": map[string]any{"timeline": map[string]any{"entries": []any{
		map[string]any{"content": map[string]any{"itemContent": item("direct")}},
		map[string]any{"content": map[string]any{"items": []any{
			map[string]any{"item": map[string]any{"itemContent": item("module-first")}},
			map[string]any{"item": map[string]any{"itemContent": item("module-second")}},
		}}},
		map[string]any{"content": map[string]any{"cursorType": "ShowMoreThreads", "value": "next-module"}},
	}}}}

	page := parseTimeline(payload)
	if page.Cursor != "next-module" {
		t.Fatalf("cursor = %q, want next-module", page.Cursor)
	}
	if len(page.Posts) != 3 {
		t.Fatalf("posts = %+v", page.Posts)
	}
	for i, want := range []string{"direct", "module-first", "module-second"} {
		if page.Posts[i].ID != want {
			t.Fatalf("post %d ID = %q, want %q", i, page.Posts[i].ID, want)
		}
	}
}
