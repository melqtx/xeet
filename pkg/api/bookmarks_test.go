package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/config"
)

const bookmarksFixture = `{
  "data": {
    "bookmark_timeline_v2": {
      "timeline": {
        "instructions": [{
          "type": "TimelineAddEntries",
          "entries": [
            {
              "entryId": "tweet-1",
              "content": {
                "entryType": "TimelineTimelineItem",
                "itemContent": {
                  "tweet_results": {
                    "result": {
                      "rest_id": "1",
                      "legacy": {"full_text": "saved post"},
                      "core": {
                        "user_results": {
                          "result": {
                            "core": {"name": "Alice", "screen_name": "alice"}
                          }
                        }
                      }
                    }
                  }
                }
              }
            },
            {
              "entryId": "cursor-bottom-1",
              "content": {
                "entryType": "TimelineTimelineCursor",
                "cursorType": "Bottom",
                "value": "CURSOR_B1"
              }
            }
          ]
        }]
      }
    }
  }
}`

func TestFetchBookmarksParsesPostsAndBottomCursorWithSpecifiedFirstPageVariables(t *testing.T) {
	client := configuredBookmarksTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/i/api/graphql/bookmarks-qid/Bookmarks" {
			t.Fatalf("path = %q, want Bookmarks GraphQL path", req.URL.Path)
		}
		variables := decodeBookmarkVariables(t, req)
		if variables["count"] != float64(25) {
			t.Fatalf("variables.count = %#v, want 25", variables["count"])
		}
		if promoted, ok := variables["includePromotedContent"].(bool); !ok || promoted {
			t.Fatalf("variables.includePromotedContent = %#v, want false", variables["includePromotedContent"])
		}
		if _, ok := variables["cursor"]; ok {
			t.Fatalf("first-page variables unexpectedly contain cursor: %#v", variables)
		}
		return response(http.StatusOK, bookmarksFixture), nil
	})

	page, err := client.FetchBookmarks(context.Background(), "", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 1 || page.Posts[0].ID != "1" || page.Posts[0].Text != "saved post" {
		t.Fatalf("posts = %+v", page.Posts)
	}
	if page.Cursor != "CURSOR_B1" {
		t.Fatalf("cursor = %q, want CURSOR_B1", page.Cursor)
	}
}

func TestFetchBookmarksPassesCursorThroughToGraphQLVariables(t *testing.T) {
	client := configuredBookmarksTestClient(t, func(req *http.Request) (*http.Response, error) {
		variables := decodeBookmarkVariables(t, req)
		if variables["cursor"] != "abc" {
			t.Fatalf("variables.cursor = %#v, want abc", variables["cursor"])
		}
		return response(http.StatusOK, bookmarksFixture), nil
	})

	if _, err := client.FetchBookmarks(context.Background(), "abc", 30); err != nil {
		t.Fatal(err)
	}
}

func TestFetchBookmarksClampsInvalidCountsToThirty(t *testing.T) {
	for _, test := range []struct {
		name  string
		count int
	}{
		{name: "zero", count: 0},
		{name: "over-maximum", count: 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := configuredBookmarksTestClient(t, func(req *http.Request) (*http.Response, error) {
				variables := decodeBookmarkVariables(t, req)
				if variables["count"] != float64(30) {
					t.Fatalf("variables.count = %#v, want 30", variables["count"])
				}
				return response(http.StatusOK, bookmarksFixture), nil
			})

			if _, err := client.FetchBookmarks(context.Background(), "", test.count); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFetchBookmarksDiscoversMissingQueryIDBeforeFirstRequestAndExposesItForPersistence(t *testing.T) {
	requests := 0
	client := NewWebClient(&config.Config{AuthToken: "auth", CT0: "csrf"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(req.URL.Path, "/fresh-bookmarks/Bookmarks") {
			t.Fatalf("path = %q", req.URL.Path)
		}
		return response(http.StatusOK, bookmarksFixture), nil
	})}
	client.discover = func(_ context.Context, auth, ct0, operation string) (string, error) {
		if auth != "auth" || ct0 != "csrf" || operation != bookmarksOperation {
			t.Fatalf("discovery args = auth:%q ct0:%q operation:%q", auth, ct0, operation)
		}
		return "fresh-bookmarks", nil
	}

	if _, err := client.FetchBookmarks(context.Background(), "", 30); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	if requests != 1 || !client.ApplyRefreshedQueryIDs(cfg) || cfg.BookmarksQID != "fresh-bookmarks" {
		t.Fatalf("requests=%d config=%+v", requests, cfg)
	}
}

func TestFetchBookmarksRebuildsIdenticalVariablesAfterQueryIDRefresh(t *testing.T) {
	var requests []map[string]any
	client := configuredBookmarksTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests = append(requests, decodeBookmarkVariables(t, req))
		if len(requests) == 1 {
			return response(http.StatusNotFound, `{}`), nil
		}
		if !strings.Contains(req.URL.Path, "/fresh-bookmarks/Bookmarks") {
			t.Fatalf("refreshed path = %q", req.URL.Path)
		}
		return response(http.StatusOK, bookmarksFixture), nil
	})
	client.discover = func(context.Context, string, string, string) (string, error) {
		return "fresh-bookmarks", nil
	}

	if _, err := client.FetchBookmarks(context.Background(), "abc", 25); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || !reflect.DeepEqual(requests[0], requests[1]) {
		t.Fatalf("variables changed across query-id refresh: %#v", requests)
	}
}

func TestFetchBookmarksRejectsMissingSessionBeforeAnyRequest(t *testing.T) {
	requests := 0
	client := NewWebClient(&config.Config{BookmarksQID: "bookmarks-qid"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return response(http.StatusOK, bookmarksFixture), nil
	})}

	if _, err := client.FetchBookmarks(context.Background(), "", 30); err == nil {
		t.Fatal("FetchBookmarks succeeded without a session")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func configuredBookmarksTestClient(t *testing.T, handler func(*http.Request) (*http.Response, error)) *WebClient {
	t.Helper()
	client := NewWebClient(&config.Config{
		AuthToken:    "auth",
		CT0:          "csrf",
		BookmarksQID: "bookmarks-qid",
	})
	client.httpClient = &http.Client{Transport: roundTripFunc(handler)}
	return client
}

func decodeBookmarkVariables(t *testing.T, req *http.Request) map[string]any {
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
