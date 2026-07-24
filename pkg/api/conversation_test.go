package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"xeet/pkg/config"
)

func TestParseConversationKeepsFocalAndDescendantsInOrder(t *testing.T) {
	item := func(id, text, parent string) map[string]any {
		legacy := map[string]any{"full_text": text}
		if parent != "" {
			legacy["in_reply_to_status_id_str"] = parent
		}
		return map[string]any{"tweet_results": map[string]any{"result": map[string]any{
			"rest_id": id, "legacy": legacy,
		}}}
	}
	entry := func(id, text, parent string) map[string]any {
		return map[string]any{"content": map[string]any{"itemContent": item(id, text, parent)}}
	}
	payload := map[string]any{"data": map[string]any{"thread": map[string]any{"instructions": []any{
		map[string]any{"entries": []any{
			entry("9", "ancestor", ""),
			entry("10", "root", "9"),
			map[string]any{"content": map[string]any{"items": []any{
				map[string]any{"item": map[string]any{"itemContent": item("11", "first", "10")}},
				map[string]any{"item": map[string]any{"itemContent": item("12", "nested", "11")}},
			}}},
			entry("20", "unrelated", "99"),
			map[string]any{"content": map[string]any{"itemContent": map[string]any{"cursorType": "ShowMoreThreads", "value": "next-replies"}}},
		}},
	}}}}
	page := parseConversation(payload, "10")
	if page.Cursor != "next-replies" || len(page.Posts) != 3 {
		t.Fatalf("page=%+v", page)
	}
	for i, want := range []struct {
		id    string
		depth int
	}{{"10", 0}, {"11", 1}, {"12", 2}} {
		if page.Posts[i].ID != want.id || page.Posts[i].Depth != want.depth {
			t.Fatalf("post %d = %+v, want id=%s depth=%d", i, page.Posts[i], want.id, want.depth)
		}
	}
}

func TestParseConversationDefersReplyWhoseParentIsOnEarlierPage(t *testing.T) {
	item := func(id, parent string) map[string]any {
		return map[string]any{"tweet_results": map[string]any{"result": map[string]any{
			"rest_id": id, "legacy": map[string]any{"full_text": id, "in_reply_to_status_id_str": parent},
		}}}
	}
	payload := map[string]any{"data": map[string]any{"thread": map[string]any{"entries": []any{
		map[string]any{"content": map[string]any{"itemContent": item("root", "")}},
		map[string]any{"content": map[string]any{"itemContent": item("nested", "parent-from-page-one")}},
	}}}}
	page := parseConversation(payload, "root")
	if len(page.Posts) != 1 || len(page.Unresolved) != 1 || page.Unresolved[0].ID != "nested" {
		t.Fatalf("page=%+v", page)
	}
}

func TestFetchTweetDetailUsesConfiguredQueryIDAndVariables(t *testing.T) {
	client := NewWebClient(&config.Config{AuthToken: "auth", CT0: "csrf", TweetDetailQID: "detail-qid"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/detail-qid/TweetDetail") {
			t.Fatalf("path=%s", req.URL.Path)
		}
		variables := req.URL.Query().Get("variables")
		for _, want := range []string{`"focalTweetId":"123"`, `"cursor":"next"`, `"count":25`} {
			if !strings.Contains(variables, want) {
				t.Fatalf("variables %s missing %s", variables, want)
			}
		}
		return response(http.StatusOK, `{"data":{"threaded_conversation_with_injections_v2":{"instructions":[]}}}`), nil
	})}
	page, err := client.FetchTweetDetail(context.Background(), "123", "next", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 0 {
		t.Fatalf("posts=%+v", page.Posts)
	}
}

func TestOperationHintKeepsTweetDetailOffTheComposeBundle(t *testing.T) {
	// The generic "Tweet" hint maps to the Compose bundle, which does not
	// contain the TweetDetail operation. Discovery must search its own chunk.
	if got := operationHint("TweetDetail"); got != "TweetDetail" {
		t.Fatalf("operationHint(TweetDetail) = %q", got)
	}
}

func TestTweetDetailRefreshesRotatedQueryID(t *testing.T) {
	attempts := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(http.StatusNotFound, `{}`), nil
		}
		if !strings.Contains(req.URL.Path, "/fresh-detail/TweetDetail") {
			t.Fatalf("retry path=%s", req.URL.Path)
		}
		return response(http.StatusOK, `{"data":{"threaded_conversation_with_injections_v2":{"instructions":[]}}}`), nil
	})
	client.operationQIDs = map[string]string{"TweetDetail": "stale-detail"}
	client.discover = func(context.Context, string, string, string) (string, error) { return "fresh-detail", nil }
	if _, err := client.FetchTweetDetail(context.Background(), "123", "", 40); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	if attempts != 2 || !client.ApplyRefreshedQueryIDs(cfg) || cfg.TweetDetailQID != "fresh-detail" {
		t.Fatalf("attempts=%d cfg=%+v", attempts, cfg)
	}
}
