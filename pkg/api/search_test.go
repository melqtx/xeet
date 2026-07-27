package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/config"
)

const searchFixture = `{
  "data": {
    "search_by_raw_query": {
      "search_timeline": {
        "timeline": {
          "instructions": [{
            "type": "TimelineAddEntries",
            "entries": [
              {
                "entryId": "tweet-direct",
                "content": {
                  "entryType": "TimelineTimelineItem",
                  "itemContent": {
                    "tweet_results": {
                      "result": {
                        "rest_id": "direct",
                        "legacy": {"full_text": "direct result"}
                      }
                    }
                  }
                }
              },
              {
                "entryId": "search-conversation",
                "content": {
                  "entryType": "TimelineTimelineModule",
                  "items": [{
                    "entryId": "search-conversation-module",
                    "item": {
                      "itemContent": {
                        "tweet_results": {
                          "result": {
                            "rest_id": "module",
                            "legacy": {"full_text": "module result"}
                          }
                        }
                      }
                    }
                  }]
                }
              },
              {
                "entryId": "cursor-bottom",
                "content": {
                  "entryType": "TimelineTimelineCursor",
                  "cursorType": "Bottom",
                  "value": "CURSOR_S1"
                }
              }
            ]
          }]
        }
      }
    }
  }
}`

func TestFetchSearchTimelineParsesDirectAndModuleResultsInEntryOrder(t *testing.T) {
	client := configuredSearchTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/i/api/graphql/search-qid/SearchTimeline" {
			t.Fatalf("path = %q, want SearchTimeline GraphQL path", req.URL.Path)
		}
		variables := decodeSearchVariables(t, req)
		for name, want := range map[string]any{
			"rawQuery":    "go tui",
			"count":       float64(25),
			"product":     "Top",
			"querySource": "typed_query",
		} {
			if got := variables[name]; got != want {
				t.Fatalf("variables.%s = %#v, want %#v", name, got, want)
			}
		}
		if _, ok := variables["cursor"]; ok {
			t.Fatalf("first-page variables unexpectedly contain cursor: %#v", variables)
		}
		return response(http.StatusOK, searchFixture), nil
	})

	page, err := client.FetchSearchTimeline(context.Background(), "go tui", "", 25)
	if err != nil {
		t.Fatal(err)
	}
	if page.Cursor != "CURSOR_S1" {
		t.Fatalf("cursor = %q, want CURSOR_S1", page.Cursor)
	}
	if len(page.Posts) != 2 {
		t.Fatalf("posts = %+v", page.Posts)
	}
	for i, want := range []struct {
		id   string
		text string
	}{
		{id: "direct", text: "direct result"},
		{id: "module", text: "module result"},
	} {
		if page.Posts[i].ID != want.id || page.Posts[i].Text != want.text {
			t.Fatalf("post %d = %+v, want id=%q text=%q", i, page.Posts[i], want.id, want.text)
		}
	}
}

func TestFetchSearchTimelinePassesCursorThroughToGraphQLVariables(t *testing.T) {
	client := configuredSearchTestClient(t, func(req *http.Request) (*http.Response, error) {
		variables := decodeSearchVariables(t, req)
		if variables["cursor"] != "next-page" {
			t.Fatalf("variables.cursor = %#v, want next-page", variables["cursor"])
		}
		return response(http.StatusOK, searchFixture), nil
	})

	if _, err := client.FetchSearchTimeline(context.Background(), "go", "next-page", 30); err != nil {
		t.Fatal(err)
	}
}

func TestFetchSearchTimelineRejectsEmptyQueryBeforeAnyRequest(t *testing.T) {
	requests := 0
	client := configuredSearchTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return response(http.StatusOK, searchFixture), nil
	})

	_, err := client.FetchSearchTimeline(context.Background(), " \t\n", "", 30)
	if err == nil || err.Error() != "search query is empty" {
		t.Fatalf("error = %v, want search query is empty", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestFetchSearchTimelineDiscoversAndExposesMissingQueryIDForPersistence(t *testing.T) {
	requests := 0
	client := NewWebClient(&config.Config{AuthToken: "auth", CT0: "csrf"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(req.URL.Path, "/fresh-search/SearchTimeline") {
			t.Fatalf("path = %q", req.URL.Path)
		}
		return response(http.StatusOK, searchFixture), nil
	})}
	client.discover = func(_ context.Context, auth, ct0, operation string) (string, error) {
		if auth != "auth" || ct0 != "csrf" || operation != searchOperation {
			t.Fatalf("discovery args = auth:%q ct0:%q operation:%q", auth, ct0, operation)
		}
		return "fresh-search", nil
	}
	client.transactionID = func(context.Context, string, string) (string, error) {
		return "stub-transaction-id", nil
	}

	if _, err := client.FetchSearchTimeline(context.Background(), "go", "", 30); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	if requests != 1 || !client.ApplyRefreshedQueryIDs(cfg) || cfg.SearchTimelineQID != "fresh-search" {
		t.Fatalf("requests=%d config=%+v", requests, cfg)
	}
}

func configuredSearchTestClient(t *testing.T, handler func(*http.Request) (*http.Response, error)) *WebClient {
	t.Helper()
	client := NewWebClient(&config.Config{
		AuthToken:         "auth",
		CT0:               "csrf",
		SearchTimelineQID: "search-qid",
	})
	client.httpClient = &http.Client{Transport: roundTripFunc(handler)}
	// The real generator derives its key by fetching x.com, which the stubbed
	// transport would answer with search fixtures.
	client.transactionID = func(context.Context, string, string) (string, error) {
		return "stub-transaction-id", nil
	}
	return client
}

func decodeSearchVariables(t *testing.T, req *http.Request) map[string]any {
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

func TestFetchSearchTimelineSendsTransactionHeaderThatBookmarksOmits(t *testing.T) {
	var searchHeader string
	search := configuredSearchTestClient(t, func(req *http.Request) (*http.Response, error) {
		searchHeader = req.Header.Get("X-Client-Transaction-Id")
		return response(http.StatusOK, searchFixture), nil
	})
	if _, err := search.FetchSearchTimeline(context.Background(), "go tui", "", 25); err != nil {
		t.Fatal(err)
	}
	// X answers SearchTimeline with a bodyless 404 when this header is absent,
	// which is indistinguishable from a dead endpoint at the call site.
	if searchHeader == "" {
		t.Fatal("search sent no X-Client-Transaction-Id; the live endpoint would 404")
	}

	var bookmarksHeader string
	bookmarks := NewWebClient(&config.Config{AuthToken: "auth", CT0: "csrf", BookmarksQID: "bookmarks-qid"})
	bookmarks.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		bookmarksHeader = req.Header.Get("X-Client-Transaction-Id")
		return response(http.StatusOK, searchFixture), nil
	})}
	bookmarks.transactionID = func(context.Context, string, string) (string, error) {
		return "stub-transaction-id", nil
	}
	if _, err := bookmarks.FetchBookmarks(context.Background(), "", 25); err != nil {
		t.Fatal(err)
	}
	if bookmarksHeader != "" {
		t.Fatalf("bookmarks sent X-Client-Transaction-Id = %q; only search needs it and minting one costs a round trip", bookmarksHeader)
	}
}
