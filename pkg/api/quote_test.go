package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestQuotePayloadAndCreatedID(t *testing.T) {
	c := newTestClient(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Variables["attachment_url"] != "https://x.com/i/status/123" || body.Variables["tweet_text"] != "my take" || body.Variables["reply"] != nil {
			t.Fatalf("quote payload: %v", body.Variables)
		}
		return response(200, `{"data":{"create_tweet":{"tweet_results":{"result":{"rest_id":"456","quoted_status_result":{"result":{"rest_id":"123"}}}}}}}`), nil
	})
	id, err := c.PostQuote(context.Background(), "my take", "123", nil, nil)
	if err != nil || id != "456" {
		t.Fatalf("id=%s err=%v", id, err)
	}
}

func TestQuoteAmbiguityDoesNotRetryOrMatchPlainPost(t *testing.T) {
	for _, transportFailure := range []bool{false, true} {
		calls := 0
		c := newTestClient(func(req *http.Request) (*http.Response, error) {
			calls++
			if transportFailure {
				return nil, errors.New("connection lost")
			}
			return response(200, `{"data":{"create_tweet":{"tweet_results":{"result":{}}}}}`), nil
		})
		_, err := c.PostQuote(context.Background(), "my take", "123", nil, nil)
		var ambiguous *AmbiguousPostError
		if !errors.As(err, &ambiguous) || calls != 1 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
}

func TestQuoteRejectsInvalidTargetsBeforeNetwork(t *testing.T) {
	c := newTestClient(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected request"); return nil, nil })
	for _, id := range []string{"", "123/other", "https://x.com/i/status/123"} {
		if _, err := c.PostQuote(context.Background(), "text", id, nil, nil); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
}

func TestQuoteStaleEndpointRetainsAttachment(t *testing.T) {
	calls := 0
	c := newTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		var body struct {
			Variables createTweetVariables `json:"variables"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		if body.Variables.AttachmentURL != "https://x.com/i/status/123" {
			t.Fatal("quote lost during retry")
		}
		if calls == 1 {
			return response(404, "gone"), nil
		}
		return response(200, `{"data":{"create_tweet":{"tweet_results":{"result":{"rest_id":"456"}}}}}`), nil
	})
	c.discover = func(context.Context, string, string, string) (string, error) { return "fresh", nil }
	if _, err := c.PostQuote(context.Background(), "text", "123", nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
