package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/config"
)

const userTweetsFixture = `{
  "data": {
    "user": {
      "result": {
        "timeline_v2": {
          "timeline": {
            "instructions": [{
              "type": "TimelineAddEntries",
              "entries": [
                {
                  "entryId": "tweet-p1",
                  "content": {
                    "entryType": "TimelineTimelineItem",
                    "itemContent": {
                      "tweet_results": {
                        "result": {
                          "rest_id": "p1",
                          "legacy": {"full_text": "first profile post"}
                        }
                      }
                    }
                  }
                },
                {
                  "entryId": "cursor-bottom",
                  "content": {
                    "entryType": "TimelineTimelineCursor",
                    "cursorType": "Bottom",
                    "value": "CURSOR_U1"
                  }
                }
              ]
            }]
          }
        }
      }
    }
  }
}`

func configuredProfileTestClient(t *testing.T, handler func(*http.Request) (*http.Response, error)) *WebClient {
	t.Helper()
	client := NewWebClient(&config.Config{
		AuthToken:           "auth",
		CT0:                 "csrf",
		UserByScreenNameQID: "lookup-qid",
		UserTweetsQID:       "usertweets-qid",
	})
	client.httpClient = &http.Client{Transport: roundTripFunc(handler)}
	client.transactionID = func(context.Context, string, string) (string, error) {
		return "stub-transaction-id", nil
	}
	return client
}

func decodeProfileVariables(t *testing.T, req *http.Request) map[string]any {
	t.Helper()
	raw := req.URL.Query().Get("variables")
	if raw == "" {
		t.Fatal("request has no variables parameter")
	}
	var variables map[string]any
	if err := json.Unmarshal([]byte(raw), &variables); err != nil {
		t.Fatalf("decode variables %q: %v", raw, err)
	}
	return variables
}

func TestFetchUserByScreenNameResolvesID(t *testing.T) {
	client := configuredProfileTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/i/api/graphql/lookup-qid/UserByScreenName" {
			t.Fatalf("path = %q, want UserByScreenName GraphQL path", req.URL.Path)
		}
		variables := decodeProfileVariables(t, req)
		if variables["screen_name"] != "someone" {
			t.Fatalf("variables.screen_name = %#v, want someone", variables["screen_name"])
		}
		return response(http.StatusOK, `{"data":{"user":{"result":{"rest_id":"12345"}}}}`), nil
	})

	id, err := client.FetchUserByScreenName(context.Background(), "@someone")
	if err != nil {
		t.Fatal(err)
	}
	if id != "12345" {
		t.Fatalf("id = %q, want 12345", id)
	}
}

func TestFetchUserByScreenNameRejectsEmptyHandleBeforeAnyRequest(t *testing.T) {
	requests := 0
	client := configuredProfileTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return response(http.StatusOK, `{}`), nil
	})

	if _, err := client.FetchUserByScreenName(context.Background(), " @ "); err == nil {
		t.Fatal("blank handle should fail")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestFetchUserByScreenNameErrorsWhenUserMissing(t *testing.T) {
	client := configuredProfileTestClient(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"data":{"user":{}}}`), nil
	})

	_, err := client.FetchUserByScreenName(context.Background(), "nobody")
	if err == nil || !strings.Contains(err.Error(), "no user for @nobody") {
		t.Fatalf("error = %v, want a no-user error naming the handle", err)
	}
}

func TestFetchUserByScreenNameDiscoversAndStagesMissingQueryID(t *testing.T) {
	requests := 0
	client := NewWebClient(&config.Config{AuthToken: "auth", CT0: "csrf"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(req.URL.Path, "/fresh-lookup/UserByScreenName") {
			t.Fatalf("path = %q", req.URL.Path)
		}
		return response(http.StatusOK, `{"data":{"user":{"result":{"rest_id":"12345"}}}}`), nil
	})}
	client.discover = func(_ context.Context, auth, ct0, operation string) (string, error) {
		if operation != userByScreenNameOperation {
			t.Fatalf("discovery operation = %q", operation)
		}
		return "fresh-lookup", nil
	}

	if _, err := client.FetchUserByScreenName(context.Background(), "someone"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	if requests != 1 || !client.ApplyRefreshedQueryIDs(cfg) || cfg.UserByScreenNameQID != "fresh-lookup" {
		t.Fatalf("requests=%d config=%+v", requests, cfg)
	}
}

func TestFetchUserByScreenNameEnvOverridesQueryID(t *testing.T) {
	t.Setenv("XEET_USERBYSCREENNAME_QID", "env-lookup")
	client := NewWebClient(&config.Config{AuthToken: "auth", CT0: "csrf", UserByScreenNameQID: "cfg-lookup"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/env-lookup/UserByScreenName") {
			t.Fatalf("path = %q, want the env override qid", req.URL.Path)
		}
		return response(http.StatusOK, `{"data":{"user":{"result":{"rest_id":"1"}}}}`), nil
	})}

	if _, err := client.FetchUserByScreenName(context.Background(), "someone"); err != nil {
		t.Fatal(err)
	}
}

func TestFetchUserTweetsSendsUserIDAndParsesPage(t *testing.T) {
	client := configuredProfileTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/i/api/graphql/usertweets-qid/UserTweets" {
			t.Fatalf("path = %q, want UserTweets GraphQL path", req.URL.Path)
		}
		variables := decodeProfileVariables(t, req)
		for name, want := range map[string]any{
			"userId":                 "12345",
			"count":                  float64(20),
			"includePromotedContent": false,
			"withV2Timeline":         true,
		} {
			if got := variables[name]; got != want {
				t.Fatalf("variables.%s = %#v, want %#v", name, got, want)
			}
		}
		if _, ok := variables["cursor"]; ok {
			t.Fatalf("first-page variables unexpectedly contain cursor: %#v", variables)
		}
		return response(http.StatusOK, userTweetsFixture), nil
	})

	page, err := client.FetchUserTweets(context.Background(), "12345", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 1 || page.Posts[0].ID != "p1" || page.Posts[0].Text != "first profile post" {
		t.Fatalf("posts = %+v", page.Posts)
	}
	if page.Cursor != "CURSOR_U1" {
		t.Fatalf("cursor = %q, want CURSOR_U1", page.Cursor)
	}
}

func TestFetchUserTweetsPassesCursorThrough(t *testing.T) {
	client := configuredProfileTestClient(t, func(req *http.Request) (*http.Response, error) {
		variables := decodeProfileVariables(t, req)
		if variables["cursor"] != "next-page" {
			t.Fatalf("variables.cursor = %#v, want next-page", variables["cursor"])
		}
		return response(http.StatusOK, userTweetsFixture), nil
	})

	if _, err := client.FetchUserTweets(context.Background(), "12345", "next-page", 30); err != nil {
		t.Fatal(err)
	}
}

func TestFetchUserTweetsRejectsEmptyUserIDBeforeAnyRequest(t *testing.T) {
	requests := 0
	client := configuredProfileTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return response(http.StatusOK, userTweetsFixture), nil
	})

	if _, err := client.FetchUserTweets(context.Background(), "  ", "", 30); err == nil {
		t.Fatal("blank user id should fail")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestFetchUserTweetsDiscoversAndStagesMissingQueryID(t *testing.T) {
	requests := 0
	client := NewWebClient(&config.Config{AuthToken: "auth", CT0: "csrf"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(req.URL.Path, "/fresh-usertweets/UserTweets") {
			t.Fatalf("path = %q", req.URL.Path)
		}
		return response(http.StatusOK, userTweetsFixture), nil
	})}
	client.discover = func(_ context.Context, auth, ct0, operation string) (string, error) {
		if operation != userTweetsOperation {
			t.Fatalf("discovery operation = %q", operation)
		}
		return "fresh-usertweets", nil
	}

	if _, err := client.FetchUserTweets(context.Background(), "12345", "", 30); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	if requests != 1 || !client.ApplyRefreshedQueryIDs(cfg) || cfg.UserTweetsQID != "fresh-usertweets" {
		t.Fatalf("requests=%d config=%+v", requests, cfg)
	}
}

func TestProfileOperationsRequireSession(t *testing.T) {
	client := &WebClient{}
	if _, err := client.FetchUserByScreenName(context.Background(), "someone"); err == nil {
		t.Fatal("lookup without a session should fail")
	}
	if _, err := client.FetchUserTweets(context.Background(), "12345", "", 30); err == nil {
		t.Fatal("user tweets without a session should fail")
	}
}
