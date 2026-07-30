package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

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

func configuredBookmarkMutationClient(t *testing.T, cfg *config.Config, handler func(*http.Request) (*http.Response, error)) *WebClient {
	t.Helper()
	cfg.AuthToken, cfg.CT0 = "auth", "csrf"
	client := NewWebClient(cfg)
	client.httpClient = &http.Client{Transport: roundTripFunc(handler)}
	client.retryDelay = time.Millisecond
	// The generator scrapes X's transaction page; tests stub the minting step
	// and assert on the header it produces instead.
	client.transactionID = func(context.Context, string, string) (string, error) {
		return "tx-id", nil
	}
	return client
}

func TestSetTweetBookmarkedPostsTweetIDToCreateBookmark(t *testing.T) {
	var path, body string
	client := configuredBookmarkMutationClient(t, &config.Config{CreateBookmarkQID: "cb-qid"}, func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return response(http.StatusOK, `{"data":{"tweet_bookmark_put":"Done"}}`), nil
	})

	if err := client.SetTweetBookmarked(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "/cb-qid/CreateBookmark") || !strings.Contains(body, `"tweet_id":"123"`) {
		t.Fatalf("path=%q body=%s", path, body)
	}
}

func TestSetTweetUnbookmarkedPostsTweetIDToDeleteBookmark(t *testing.T) {
	var path, body string
	client := configuredBookmarkMutationClient(t, &config.Config{DeleteBookmarkQID: "db-qid"}, func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return response(http.StatusOK, `{"data":{"tweet_bookmark_delete":"Done"}}`), nil
	})

	if err := client.SetTweetBookmarked(context.Background(), "123", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "/db-qid/DeleteBookmark") || !strings.Contains(body, `"tweet_id":"123"`) {
		t.Fatalf("path=%q body=%s", path, body)
	}
}

// The bookmark mutations answer a bodyless 404 without the transaction
// header — confirmed on the first live run — so every attempt must carry the
// minted id, exactly like the retweet pair.
func TestSetTweetBookmarkedSendsTransactionHeader(t *testing.T) {
	var header string
	client := configuredBookmarkMutationClient(t, &config.Config{CreateBookmarkQID: "cb-qid"}, func(req *http.Request) (*http.Response, error) {
		header = req.Header.Get("X-Client-Transaction-Id")
		return response(http.StatusOK, `{"data":{"tweet_bookmark_put":"Done"}}`), nil
	})

	if err := client.SetTweetBookmarked(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if header != "tx-id" {
		t.Fatalf("X-Client-Transaction-Id = %q, want tx-id", header)
	}
}

func TestSetTweetBookmarkedDiscoversMissingQueryIDBeforeFirstRequestAndExposesItForPersistence(t *testing.T) {
	var path string
	client := configuredBookmarkMutationClient(t, &config.Config{}, func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		return response(http.StatusOK, `{"data":{"tweet_bookmark_put":"Done"}}`), nil
	})
	client.discover = func(_ context.Context, auth, ct0, operation string) (string, error) {
		if auth != "auth" || ct0 != "csrf" || operation != createBookmarkOperation {
			t.Fatalf("discovery args = auth:%q ct0:%q operation:%q", auth, ct0, operation)
		}
		return "fresh-cb", nil
	}

	if err := client.SetTweetBookmarked(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "/fresh-cb/CreateBookmark") {
		t.Fatalf("path=%q", path)
	}
	cfg := &config.Config{}
	if !client.ApplyRefreshedQueryIDs(cfg) || cfg.CreateBookmarkQID != "fresh-cb" {
		t.Fatalf("config=%+v", cfg)
	}
}

func TestSetTweetBookmarkedRotatesQueryIDOnceOnPersistedQueryMiss(t *testing.T) {
	var paths []string
	client := configuredBookmarkMutationClient(t, &config.Config{DeleteBookmarkQID: "stale"}, func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if len(paths) == 1 {
			return response(http.StatusNotFound, ``), nil
		}
		return response(http.StatusOK, `{"data":{"tweet_bookmark_delete":"Done"}}`), nil
	})
	discoveries := 0
	client.discover = func(_ context.Context, _, _, operation string) (string, error) {
		discoveries++
		return "fresh-db", nil
	}

	if err := client.SetTweetBookmarked(context.Background(), "123", false); err != nil {
		t.Fatal(err)
	}
	if discoveries != 1 || len(paths) != 2 ||
		!strings.Contains(paths[0], "/stale/DeleteBookmark") || !strings.Contains(paths[1], "/fresh-db/DeleteBookmark") {
		t.Fatalf("discoveries=%d paths=%v", discoveries, paths)
	}
}

// Bookmarks are only half idempotent: re-adding one is a no-op, but the live
// endpoint errors on a replayed delete ("_Missing: not found in actor's
// favorites"). So a create rides out a transient 503 while a delete surfaces
// after exactly one attempt and leaves the retry to the user.
func TestSetTweetBookmarkedRetriesTransientStatus(t *testing.T) {
	attempts := 0
	client := configuredBookmarkMutationClient(t, &config.Config{CreateBookmarkQID: "cb-qid"}, func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(http.StatusServiceUnavailable, ``), nil
		}
		return response(http.StatusOK, `{"data":{"tweet_bookmark_put":"Done"}}`), nil
	})

	if err := client.SetTweetBookmarked(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (transient retry for idempotent bookmarks)", attempts)
	}
}

func TestSetTweetBookmarkedSurfacesPersistentTransientStatus(t *testing.T) {
	client := configuredBookmarkMutationClient(t, &config.Config{CreateBookmarkQID: "cb-qid"}, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusServiceUnavailable, ``), nil
	})

	err := client.SetTweetBookmarked(context.Background(), "123", true)
	var unavailable *ServiceUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("err = %v, want ServiceUnavailableError", err)
	}
}

func TestSetTweetUnbookmarkedDoesNotRetryTransientStatus(t *testing.T) {
	attempts := 0
	client := configuredBookmarkMutationClient(t, &config.Config{DeleteBookmarkQID: "db-qid"}, func(req *http.Request) (*http.Response, error) {
		attempts++
		return response(http.StatusServiceUnavailable, ``), nil
	})

	err := client.SetTweetBookmarked(context.Background(), "123", false)
	var unavailable *ServiceUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("err = %v, want ServiceUnavailableError", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (no transient retry for bookmark delete)", attempts)
	}
}

func TestSetTweetBookmarkedHonorsEnvironmentQueryIDOverride(t *testing.T) {
	t.Setenv("XEET_CREATEBOOKMARK_QID", "env-cb")
	var path string
	client := configuredBookmarkMutationClient(t, &config.Config{CreateBookmarkQID: "cfg-cb"}, func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		return response(http.StatusOK, `{"data":{"tweet_bookmark_put":"Done"}}`), nil
	})

	if err := client.SetTweetBookmarked(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "/env-cb/CreateBookmark") {
		t.Fatalf("path=%q", path)
	}
}

func TestSetTweetBookmarkedRejectsMissingSessionAndEmptyIDBeforeAnyRequest(t *testing.T) {
	requests := 0
	client := configuredBookmarkMutationClient(t, &config.Config{CreateBookmarkQID: "cb-qid"}, func(req *http.Request) (*http.Response, error) {
		requests++
		return response(http.StatusOK, `{}`), nil
	})
	client.authToken, client.ct0 = "", ""

	if err := client.SetTweetBookmarked(context.Background(), "123", true); err == nil {
		t.Fatal("SetTweetBookmarked succeeded without a session")
	}
	client.authToken, client.ct0 = "auth", "csrf"
	if err := client.SetTweetBookmarked(context.Background(), "", true); err == nil {
		t.Fatal("SetTweetBookmarked succeeded with an empty tweet id")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}
