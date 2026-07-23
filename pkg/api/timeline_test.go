package api

import (
	"encoding/json"
	"testing"
)

func TestParseTimeline(t *testing.T) {
	fixture := `[
	  {"itemContent":{"tweet_results":{"result":{
	    "rest_id":"1",
	    "legacy":{"full_text":"hello timeline","reply_count":2,"retweet_count":3,"favorite_count":4,"favorited":true,"created_at":"Mon Jul 20 10:00:00 +0000 2026","extended_entities":{"media":[{"type":"photo","media_url_https":"https://pbs.twimg.com/media/abc","ext_alt_text":"a cat","original_info":{"width":1200,"height":800}}]}},
	    "views":{"count":"55"},
	    "core":{"user_results":{"result":{"is_blue_verified":true,"core":{"name":"Alice","screen_name":"alice"}}}}
	  }}}},
	  {"itemContent":{"tweet_results":{"result":{"tweet":{
	    "rest_id":"2","legacy":{"full_text":"short"},
	    "note_tweet":{"note_tweet_results":{"result":{"text":"this is the long note"}}},
	    "core":{"user_results":{"result":{"legacy":{"name":"Bob","screen_name":"bob"}}}}
	  }}}}},
	  {"cursorType":"Bottom","value":"next-page"}
	]`
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
	if len(first.Media) != 1 || first.Media[0].URL != "https://pbs.twimg.com/media/abc" ||
		first.Media[0].AltText != "a cat" || first.Media[0].Width != 1200 || first.Media[0].Height != 800 {
		t.Fatalf("unexpected media: %+v", first.Media)
	}
	if page.Posts[1].Text != "this is the long note" || page.Posts[1].Handle != "bob" {
		t.Fatalf("unexpected wrapped post: %+v", page.Posts[1])
	}
}

func TestParseTimelineDeduplicatesTweet(t *testing.T) {
	item := map[string]any{"itemContent": map[string]any{"tweet_results": map[string]any{"result": map[string]any{"rest_id": "1", "legacy": map[string]any{"full_text": "same"}}}}}
	page := parseTimeline([]any{item, item})
	if len(page.Posts) != 1 {
		t.Fatalf("got %d posts", len(page.Posts))
	}
}
