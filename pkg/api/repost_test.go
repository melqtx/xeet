package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRepostAndUndoPayloads(t *testing.T) {
	for _, reposted := range []bool{true, false} {
		client := &WebClient{authToken: "auth", ct0: "csrf", operationQIDs: map[string]string{"CreateRetweet": "create", "DeleteRetweet": "delete"}}
		client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			vars := payload["variables"].(map[string]any)
			if vars["dark_request"] != false {
				t.Fatal("missing dark_request")
			}
			if reposted {
				if !strings.HasSuffix(req.URL.Path, "/CreateRetweet") || vars["tweet_id"] != "123" {
					t.Fatalf("create payload=%s", body)
				}
				return response(200, `{"data":{"create_retweet":{"retweet_results":{"result":{"rest_id":"456"}}}}}`), nil
			}
			if !strings.HasSuffix(req.URL.Path, "/DeleteRetweet") || vars["source_tweet_id"] != "123" || vars["tweet_id"] != nil {
				t.Fatalf("undo payload=%s", body)
			}
			return response(200, `{"data":{"unretweet":{"source_tweet_results":{"result":{"rest_id":"123"}}}}}`), nil
		})}
		if err := client.SetTweetReposted(context.Background(), "123", reposted); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRepostNeverAcceptsOrRetriesUnconfirmedResults(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{200, `{}`}, {200, `{"errors":[{"code":226,"message":"blocked"}]}`}, {503, `{}`}, {200, `not-json`}} {
		calls := 0
		client := &WebClient{authToken: "auth", ct0: "csrf", operationQIDs: map[string]string{"CreateRetweet": "create"}}
		client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(tc.status, tc.body), nil })}
		if err := client.SetTweetReposted(context.Background(), "123", true); err == nil || calls != 1 {
			t.Fatalf("status=%d calls=%d err=%v", tc.status, calls, err)
		}
	}
}

func TestRepostRefreshesRotatedEndpointOnce(t *testing.T) {
	calls := 0
	client := &WebClient{authToken: "auth", ct0: "csrf", operationQIDs: map[string]string{"CreateRetweet": "stale"}}
	client.discover = func(_ context.Context, _, _, operation string) (string, error) {
		if operation != "CreateRetweet" {
			t.Fatal(operation)
		}
		return "fresh", nil
	}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(404, `{}`), nil
		}
		if !strings.Contains(req.URL.Path, "/fresh/") {
			t.Fatal("stale endpoint reused")
		}
		return response(200, `{"data":{"create_retweet":{"retweet_results":{"result":{"rest_id":"456"}}}}}`), nil
	})}
	if err := client.SetTweetReposted(context.Background(), "123", true); err != nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
