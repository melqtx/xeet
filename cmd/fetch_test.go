package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/api"
)

func TestTweetIDFromArg(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{name: "bare id", arg: "1234567890", want: "1234567890"},
		{name: "x.com status", arg: "https://x.com/alice/status/1234567890", want: "1234567890"},
		{name: "twitter.com status", arg: "https://twitter.com/alice/status/1234567890", want: "1234567890"},
		{name: "www prefix", arg: "https://www.x.com/alice/status/1234567890", want: "1234567890"},
		{name: "mobile prefix", arg: "https://mobile.twitter.com/alice/status/1234567890", want: "1234567890"},
		{name: "statuses plural", arg: "https://x.com/alice/statuses/1234567890", want: "1234567890"},
		{name: "tracking query", arg: "https://x.com/alice/status/1234567890?s=20&t=abc", want: "1234567890"},
		{name: "photo suffix", arg: "https://x.com/alice/status/1234567890/photo/1", want: "1234567890"},
		{name: "analytics suffix", arg: "https://x.com/alice/status/1234567890/analytics", want: "1234567890"},
		{name: "http scheme", arg: "http://x.com/alice/status/1234567890", want: "1234567890"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := tweetIDFromArg(test.arg)
			if err != nil {
				t.Fatalf("tweetIDFromArg(%q): %v", test.arg, err)
			}
			if got != test.want {
				t.Fatalf("tweetIDFromArg(%q) = %q, want %q", test.arg, got, test.want)
			}
		})
	}
}

func TestTweetIDFromArgRejectsNonStatusInput(t *testing.T) {
	for _, arg := range []string{
		"",
		"   ",
		"not-a-url",
		"https://example.com/alice/status/123",
		"https://xcom.evil.example/alice/status/123",
		"https://x.com/alice",
		"https://x.com/home",
		"https://x.com/alice/status/abc",
	} {
		if id, err := tweetIDFromArg(arg); err == nil {
			t.Fatalf("tweetIDFromArg(%q) = %q, want error", arg, id)
		}
	}
}

func TestRenderFetchJSONEmitsStableAgentSchema(t *testing.T) {
	focal := api.TimelinePost{
		ID:         "123",
		Text:       "hello agents",
		AuthorName: "Alice",
		Handle:     "alice",
		CreatedAt:  time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC),
		ReplyCount: 2, RepostCount: 3, LikeCount: 4,
		ViewCount:  "55",
		Media:      []api.TimelineMedia{{URL: "https://pbs.twimg.com/media/abc", Type: "photo", AltText: "a cat"}},
		Article:    &api.TimelineArticle{Title: "My Article", Text: "the full body"},
		Bookmarked: true,
	}
	replies := []api.TimelinePost{{ID: "124", Text: "first reply", Handle: "bob", InReplyToID: "123"}}

	data, err := renderFetchJSON(focal, replies)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, data)
	}
	if decoded["id"] != "123" || decoded["url"] != "https://x.com/alice/status/123" {
		t.Fatalf("id/url wrong: %v", decoded)
	}
	author, _ := decoded["author"].(map[string]any)
	if author["handle"] != "alice" || author["name"] != "Alice" {
		t.Fatalf("author wrong: %v", author)
	}
	if decoded["created_at"] != "2026-07-20T10:00:00Z" {
		t.Fatalf("created_at = %v", decoded["created_at"])
	}
	article, _ := decoded["article"].(map[string]any)
	if article["title"] != "My Article" || article["text"] != "the full body" {
		t.Fatalf("article wrong: %v", article)
	}
	media, _ := decoded["media"].([]any)
	if len(media) != 1 || media[0].(map[string]any)["alt_text"] != "a cat" {
		t.Fatalf("media wrong: %v", media)
	}
	replyList, _ := decoded["replies"].([]any)
	if len(replyList) != 1 || replyList[0].(map[string]any)["in_reply_to_id"] != "123" {
		t.Fatalf("replies wrong: %v", replyList)
	}
	if decoded["bookmarked"] != true || decoded["reply_count"] != float64(2) {
		t.Fatalf("flags/counts wrong: %v", decoded)
	}
}

func TestRenderFetchJSONOmitsAbsentArticleAndReplies(t *testing.T) {
	data, err := renderFetchJSON(api.TimelinePost{ID: "1", Text: "plain", Handle: "alice"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["article"]; ok {
		t.Fatalf("article key should be omitted: %s", data)
	}
	if _, ok := decoded["replies"]; ok {
		t.Fatalf("replies key should be omitted: %s", data)
	}
}

func TestRenderFetchTextIncludesBodyCountsAndURL(t *testing.T) {
	focal := api.TimelinePost{
		ID: "123", Text: "hello", AuthorName: "Alice", Handle: "alice",
		CreatedAt: time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC),
		LikeCount: 4, RepostCount: 3, ReplyCount: 2, ViewCount: "55",
		Article: &api.TimelineArticle{Title: "My Article", Text: "the full body"},
	}
	out := renderFetchText(focal, []api.TimelinePost{{ID: "124", Text: "reply", AuthorName: "Bob", Handle: "bob"}})
	for _, want := range []string{
		"Alice (@alice)", "hello", "[Article: My Article]", "the full body",
		"♥ 4 · ⟳ 3 · 💬 2", "views 55", "https://x.com/alice/status/123", "↳", "Bob (@bob)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("text output missing %q:\n%s", want, out)
		}
	}
}
