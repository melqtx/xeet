package api

import (
	"context"
	"encoding/json"
	"github.com/melqtx/xeet/pkg/config"
	"net/http"
	"testing"
)

func TestProfileLookupRefreshesAndParses(t *testing.T) {
	calls := 0
	c := newTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(404, "gone"), nil
		}
		var vars map[string]any
		_ = json.Unmarshal([]byte(req.URL.Query().Get("variables")), &vars)
		if vars["screen_name"] != "alice" {
			t.Fatalf("variables: %v", vars)
		}
		return response(200, `{"data":{"user":{"result":{"rest_id":"42","core":{"name":"Alice","screen_name":"alice","created_at":"Mon Jul 20 10:00:00 +0000 2026"},"legacy":{"description":"cats &amp; code","followers_count":1200,"friends_count":12,"statuses_count":34,"protected":true,"url":"https://t.co/x","entities":{"url":{"urls":[{"expanded_url":"https://example.com"}]}}},"location":{"location":"the internet"}}}}}`), nil
	})
	c.operationQIDs["UserByScreenName"] = "old"
	c.discover = func(context.Context, string, string, string) (string, error) { return "fresh", nil }
	p, err := c.FetchProfile(context.Background(), "@alice")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "42" || p.Bio != "cats & code" || p.Followers != 1200 || p.Following != 12 || !p.Protected || p.Website != "https://example.com" || p.Location != "the internet" || p.Joined == "" {
		t.Fatalf("profile: %+v", p)
	}
	cfg := &config.Config{}
	if !c.ApplyRefreshedQueryIDs(cfg) || cfg.UserByScreenNameQID != "fresh" {
		t.Fatal("query id not persisted")
	}
}

func TestProfileUnavailableAndGraphQLErrors(t *testing.T) {
	for _, body := range []string{`{"data":{"user":{"result":{"__typename":"UserUnavailable"}}}}`, `{"data":null,"errors":[{"message":"not found","code":50}]}`, `{}`} {
		c := newTestClient(func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		c.operationQIDs["UserByScreenName"] = "test"
		if _, err := c.FetchProfile(context.Background(), "alice"); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestProfilePostsPagination(t *testing.T) {
	c := newTestClient(func(req *http.Request) (*http.Response, error) {
		var vars map[string]any
		_ = json.Unmarshal([]byte(req.URL.Query().Get("variables")), &vars)
		if vars["userId"] != "42" || vars["cursor"] != "next" || vars["includePromotedContent"] != false {
			t.Fatalf("variables: %v", vars)
		}
		return response(200, `{"data":{"user":{"result":{"timeline":{"timeline":{"instructions":[{"entries":[{"content":{"itemContent":{"tweet_results":{"result":{"rest_id":"123","legacy":{"full_text":"hello"}}}}}},{"content":{"cursorType":"Bottom","value":"after"}}]}]}}}}}}`), nil
	})
	c.operationQIDs["UserTweets"] = "test"
	page, err := c.FetchProfilePosts(context.Background(), "42", "next", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 1 || page.Posts[0].ID != "123" || page.Cursor != "after" {
		t.Fatalf("page: %+v", page)
	}
}

func TestProfileModernFieldsWithoutLegacy(t *testing.T) {
	// Mirrors the split field structure observed in a live UserByScreenName response.
	var result map[string]any
	err := json.Unmarshal([]byte(`{"rest_id":"42","core":{"name":"Alice","screen_name":"alice","created_at":"Fri Sep 29 20:18:58 +0000 2023"},"profile_bio":{"description":"cats &amp; code","entities":{"url":{"urls":[{"url":"https://t.co/x","expanded_url":"https://example.com"}]}}},"website":{"url":"https://t.co/x"},"location":{"location":"the internet"},"relationship_counts":{"followers":7322,"following":1209},"tweet_counts":{"tweets":27307},"privacy":{"protected":true},"verification":{"verified":true}}`), &result)
	if err != nil {
		t.Fatal(err)
	}
	p, err := parseProfileResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if p.Bio != "cats & code" || p.Website != "https://example.com" || p.Location != "the internet" || p.Joined == "" || p.Followers != 7322 || p.Following != 1209 || p.Posts != 27307 || !p.HasFollowers || !p.HasFollowing || !p.HasPosts || !p.Protected || !p.Verified {
		t.Fatalf("profile: %+v", p)
	}
}

func TestProfileCountsDistinguishZeroFromMissing(t *testing.T) {
	base := map[string]any{"rest_id": "42", "core": map[string]any{"name": "Alice", "screen_name": "alice"}}
	p, err := parseProfileResult(base)
	if err != nil {
		t.Fatal(err)
	}
	if p.HasFollowers || p.HasFollowing || p.HasPosts {
		t.Fatal("missing counts treated as zero")
	}
	base["legacy"] = map[string]any{"followers_count": float64(9), "friends_count": float64(4), "statuses_count": float64(8)}
	base["relationship_counts"] = map[string]any{"followers": float64(0)}
	p, err = parseProfileResult(base)
	if err != nil {
		t.Fatal(err)
	}
	if !p.HasFollowers || p.Followers != 0 || !p.HasFollowing || p.Following != 4 || !p.HasPosts || p.Posts != 8 {
		t.Fatalf("presence or fallback incorrect: %+v", p)
	}
}

func TestProfileRelationships(t *testing.T) {
	result := map[string]any{"rest_id": "42", "core": map[string]any{"name": "Alice", "screen_name": "alice"}}
	p, err := parseProfileResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if p.YouFollow || p.FollowsYou {
		t.Fatal("missing relationships must not produce badges")
	}
	result["legacy"] = map[string]any{"following": true, "followed_by": true}
	p, err = parseProfileResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if !p.YouFollow || !p.FollowsYou {
		t.Fatal("explicit relationships not parsed")
	}
}
