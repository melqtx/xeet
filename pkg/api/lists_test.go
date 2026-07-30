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

const listTimelineFixture = `{
  "data": {
    "list": {
      "tweets_timeline": {
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
                        "legacy": {"full_text": "list post"},
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
                  "value": "CURSOR_L1"
                }
              }
            ]
          }]
        }
      }
    }
  }
}`

const listsManagementFixture = `{
  "data": {
    "viewer": {
      "list_management_timeline": {
        "timeline": {
          "instructions": [{
            "type": "TimelineAddEntries",
            "entries": [
              {
                "entryId": "list-100",
                "content": {
                  "entryType": "TimelineTimelineItem",
                  "itemContent": {
                    "itemType": "TimelineTwitterList",
                    "twitter_list": {
                      "rest_id": "100",
                      "legacy": {
                        "name": "Private team",
                        "member_count": 12,
                        "mode": "Private"
                      }
                    }
                  }
                }
              },
              {
                "entryId": "lists-module",
                "content": {
                  "entryType": "TimelineTimelineModule",
                  "items": [{
                    "entryId": "list-200",
                    "item": {
                      "itemContent": {
                        "itemType": "TimelineTwitterList",
                        "list": {
                          "id_str": "200",
                          "name": "Public news",
                          "member_count": 34,
                          "mode": "Public"
                        }
                      }
                    }
                  }]
                }
              }
            ]
          }]
        }
      }
    }
  }
}`

func TestFetchListTimelineParsesPostsAndCursor(t *testing.T) {
	client := configuredListsTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/i/api/graphql/list-timeline-qid/ListLatestTweetsTimeline" {
			t.Fatalf("path = %q, want ListLatestTweetsTimeline GraphQL path", req.URL.Path)
		}
		variables := decodeListVariables(t, req)
		if variables["count"] != float64(25) {
			t.Fatalf("variables.count = %#v, want 25", variables["count"])
		}
		if _, ok := variables["cursor"]; ok {
			t.Fatalf("first-page variables unexpectedly contain cursor: %#v", variables)
		}
		return response(http.StatusOK, listTimelineFixture), nil
	})

	page, err := client.FetchListTimeline(context.Background(), "42", "", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 1 || page.Posts[0].ID != "1" || page.Posts[0].Text != "list post" {
		t.Fatalf("posts = %+v", page.Posts)
	}
	if page.Cursor != "CURSOR_L1" {
		t.Fatalf("cursor = %q, want CURSOR_L1", page.Cursor)
	}
}

func TestFetchListTimelineSendsListIDVariable(t *testing.T) {
	client := configuredListsTestClient(t, func(req *http.Request) (*http.Response, error) {
		variables := decodeListVariables(t, req)
		if variables["listId"] != "1234567890" {
			t.Fatalf("variables.listId = %#v, want 1234567890", variables["listId"])
		}
		if variables["cursor"] != "next-page" {
			t.Fatalf("variables.cursor = %#v, want next-page", variables["cursor"])
		}
		return response(http.StatusOK, listTimelineFixture), nil
	})

	if _, err := client.FetchListTimeline(context.Background(), "1234567890", "next-page", 30); err != nil {
		t.Fatal(err)
	}
}

func TestFetchOwnedListsParsesIDNameAndPrivacy(t *testing.T) {
	client := configuredListsTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/i/api/graphql/lists-management-qid/ListsManagementPageTimeline" {
			t.Fatalf("path = %q, want ListsManagementPageTimeline GraphQL path", req.URL.Path)
		}
		variables := decodeListVariables(t, req)
		if !reflect.DeepEqual(variables, map[string]any{"count": float64(100)}) {
			t.Fatalf("variables = %#v, want count=100 only", variables)
		}
		return response(http.StatusOK, listsManagementFixture), nil
	})

	lists, err := client.FetchOwnedLists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []ListInfo{
		{ID: "100", Name: "Private team", MemberCount: 12, IsPrivate: true},
		{ID: "200", Name: "Public news", MemberCount: 34, IsPrivate: false},
	}
	if !reflect.DeepEqual(lists, want) {
		t.Fatalf("lists = %+v, want %+v", lists, want)
	}
}

func TestFetchListTimelineDiscoversQueryIDWhenUnset(t *testing.T) {
	requests := 0
	client := NewWebClient(&config.Config{AuthToken: "auth", CT0: "csrf"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(req.URL.Path, "/fresh-list-timeline/ListLatestTweetsTimeline") {
			t.Fatalf("path = %q", req.URL.Path)
		}
		return response(http.StatusOK, listTimelineFixture), nil
	})}
	client.discover = func(_ context.Context, auth, ct0, operation string) (string, error) {
		if auth != "auth" || ct0 != "csrf" || operation != listLatestTweetsOperation {
			t.Fatalf("discovery args = auth:%q ct0:%q operation:%q", auth, ct0, operation)
		}
		return "fresh-list-timeline", nil
	}

	if _, err := client.FetchListTimeline(context.Background(), "42", "", 30); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	if requests != 1 || !client.ApplyRefreshedQueryIDs(cfg) || cfg.ListLatestTweetsTimelineQID != "fresh-list-timeline" {
		t.Fatalf("requests=%d config=%+v", requests, cfg)
	}
}

func TestFetchOwnedListsDiscoversQueryIDWhenUnset(t *testing.T) {
	requests := 0
	client := NewWebClient(&config.Config{AuthToken: "auth", CT0: "csrf"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(req.URL.Path, "/fresh-lists-management/ListsManagementPageTimeline") {
			t.Fatalf("path = %q", req.URL.Path)
		}
		return response(http.StatusOK, listsManagementFixture), nil
	})}
	client.discover = func(_ context.Context, auth, ct0, operation string) (string, error) {
		if auth != "auth" || ct0 != "csrf" || operation != listsManagementPageOperation {
			t.Fatalf("discovery args = auth:%q ct0:%q operation:%q", auth, ct0, operation)
		}
		return "fresh-lists-management", nil
	}

	if _, err := client.FetchOwnedLists(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	if requests != 1 || !client.ApplyRefreshedQueryIDs(cfg) ||
		cfg.ListsManagementPageTimelineQID != "fresh-lists-management" {
		t.Fatalf("requests=%d config=%+v", requests, cfg)
	}
}

func configuredListsTestClient(t *testing.T, handler func(*http.Request) (*http.Response, error)) *WebClient {
	t.Helper()
	client := NewWebClient(&config.Config{
		AuthToken:                      "auth",
		CT0:                            "csrf",
		ListLatestTweetsTimelineQID:    "list-timeline-qid",
		ListsManagementPageTimelineQID: "lists-management-qid",
	})
	client.httpClient = &http.Client{Transport: roundTripFunc(handler)}
	return client
}

func decodeListVariables(t *testing.T, req *http.Request) map[string]any {
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
