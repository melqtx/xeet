package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

// This fixture is a best-effort guess at the NotificationsTimeline shape; the
// XEET_LIVE_NOTIFICATIONS run is what proves it, and structural corrections
// land here from that response, never from invention.
const notificationsFixture = `{
  "data": {
    "notifications_timeline": {
      "timeline": {
        "instructions": [{
          "type": "TimelineAddEntries",
          "entries": [
            {
              "entryId": "notification-1",
              "content": {
                "entryType": "TimelineTimelineItem",
                "itemContent": {
                  "__typename": "TimelineNotification",
                  "notificationType": "Like",
                  "notification": {
                    "id": "notif-1",
                    "timestamp_ms": "1753700000000",
                    "message": {"text": "@alice liked your post"},
                    "notification_url": {"url": "https://x.com/bob/status/111222333"}
                  }
                }
              }
            },
            {
              "entryId": "notification-2",
              "content": {
                "entryType": "TimelineTimelineItem",
                "itemContent": {
                  "__typename": "TimelineNotification",
                  "notificationType": "Reply",
                  "notification": {
                    "id": "notif-2",
                    "timestamp_ms": "1753700100000",
                    "message": {"text": "@carol replied to your post"}
                  },
                  "tweet_results": {
                    "result": {
                      "rest_id": "999",
                      "legacy": {
                        "full_text": "reply text",
                        "favorite_count": 4,
                        "favorited": true
                      },
                      "core": {
                        "user_results": {
                          "result": {
                            "core": {"name": "Carol", "screen_name": "carol"}
                          }
                        }
                      }
                    }
                  }
                }
              }
            },
            {
              "entryId": "notification-3",
              "content": {
                "entryType": "TimelineTimelineItem",
                "itemContent": {
                  "__typename": "TimelineNotification",
                  "notificationType": "Follow",
                  "notification": {
                    "id": "notif-3",
                    "timestamp_ms": "1753700200000",
                    "message": {"text": "@dave followed you"}
                  }
                }
              }
            },
            {
              "entryId": "tweet-mention-1",
              "content": {
                "entryType": "TimelineTimelineItem",
                "itemContent": {
                  "__typename": "TimelineTweet",
                  "tweet_results": {
                    "result": {
                      "rest_id": "555",
                      "legacy": {"full_text": "@me hello there"},
                      "core": {
                        "user_results": {
                          "result": {
                            "core": {"name": "Eve", "screen_name": "eve"}
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
                "value": "CURSOR_N1"
              }
            }
          ]
        }]
      }
    }
  }
}`

func TestFetchNotificationsTimelineParsesPostsAndBottomCursorWithSpecifiedFirstPageVariables(t *testing.T) {
	client := configuredNotificationsTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/i/api/graphql/notif-qid/NotificationsTimeline" {
			t.Fatalf("path = %q, want NotificationsTimeline GraphQL path", req.URL.Path)
		}
		variables := decodeNotificationVariables(t, req)
		if variables["count"] != float64(25) {
			t.Fatalf("variables.count = %#v, want 25", variables["count"])
		}
		if _, ok := variables["cursor"]; ok {
			t.Fatalf("first-page variables unexpectedly contain cursor: %#v", variables)
		}
		return response(http.StatusOK, notificationsFixture), nil
	})

	page, err := client.FetchNotificationsTimeline(context.Background(), "", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 3 {
		t.Fatalf("posts = %+v, want 3 (the follow notification has no target post)", page.Posts)
	}
	like := page.Posts[0]
	if like.ID != "111222333" || like.Handle != "bob" || like.Text != "@alice liked your post" {
		t.Fatalf("like notification = %+v", like)
	}
	if like.CreatedAt != time.UnixMilli(1753700000000) {
		t.Fatalf("like timestamp = %v", like.CreatedAt)
	}
	reply := page.Posts[1]
	if reply.ID != "999" || reply.Handle != "carol" {
		t.Fatalf("reply notification = %+v", reply)
	}
	if reply.Text != "@carol replied to your post\n\nreply text" {
		t.Fatalf("reply text = %q", reply.Text)
	}
	if !reply.Liked || reply.LikeCount != 4 {
		t.Fatalf("embedded tweet state lost: %+v", reply)
	}
	mention := page.Posts[2]
	if mention.ID != "555" || mention.Handle != "eve" || mention.Text != "@me hello there" {
		// Mentions arrive as plain TimelineTweet entries; the typename must not
		// leak into the text as a fake prefix.
		t.Fatalf("mention entry = %+v", mention)
	}
	if page.Cursor != "CURSOR_N1" {
		t.Fatalf("cursor = %q, want CURSOR_N1", page.Cursor)
	}
}

func TestFetchNotificationsTimelinePassesCursorThroughToGraphQLVariables(t *testing.T) {
	client := configuredNotificationsTestClient(t, func(req *http.Request) (*http.Response, error) {
		variables := decodeNotificationVariables(t, req)
		if variables["cursor"] != "abc" {
			t.Fatalf("variables.cursor = %#v, want abc", variables["cursor"])
		}
		return response(http.StatusOK, notificationsFixture), nil
	})

	if _, err := client.FetchNotificationsTimeline(context.Background(), "abc", 30); err != nil {
		t.Fatal(err)
	}
}

func TestFetchNotificationsTimelineDiscoversMissingQueryIDBeforeFirstRequestAndExposesItForPersistence(t *testing.T) {
	requests := 0
	client := NewWebClient(&config.Config{AuthToken: "auth", CT0: "csrf"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(req.URL.Path, "/fresh-notif/NotificationsTimeline") {
			t.Fatalf("path = %q", req.URL.Path)
		}
		return response(http.StatusOK, notificationsFixture), nil
	})}
	client.discover = func(_ context.Context, auth, ct0, operation string) (string, error) {
		if auth != "auth" || ct0 != "csrf" || operation != notificationsTimelineOperation {
			t.Fatalf("discovery args = auth:%q ct0:%q operation:%q", auth, ct0, operation)
		}
		return "fresh-notif", nil
	}

	if _, err := client.FetchNotificationsTimeline(context.Background(), "", 30); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	if requests != 1 || !client.ApplyRefreshedQueryIDs(cfg) || cfg.NotificationsTimelineQID != "fresh-notif" {
		t.Fatalf("requests=%d config=%+v", requests, cfg)
	}
}

func TestFetchNotificationsTimelineRejectsMissingSessionBeforeAnyRequest(t *testing.T) {
	requests := 0
	client := NewWebClient(&config.Config{NotificationsTimelineQID: "notif-qid"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return response(http.StatusOK, notificationsFixture), nil
	})}

	if _, err := client.FetchNotificationsTimeline(context.Background(), "", 30); err == nil {
		t.Fatal("FetchNotificationsTimeline succeeded without a session")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestParseNotificationsPageCollapsesRepeatedNotificationsForOnePost(t *testing.T) {
	payload := decodeFixture(t, `{
	  "data": {"timeline": {"instructions": [{"entries": [
	    {"content": {"itemContent": {
	      "notificationType": "Like",
	      "notification": {"message": {"text": "@alice liked your post"},
	        "notification_url": {"url": "https://x.com/bob/status/42"}}}}},
	    {"content": {"itemContent": {
	      "notificationType": "Retweet",
	      "notification": {"message": {"text": "@carol reposted your post"},
	        "notification_url": {"url": "https://x.com/bob/status/42"}}}}}
	  ]}]}}
	}`)

	page := parseNotificationsPage(payload)
	// The post id is the dedup key everywhere downstream (merge, like, image
	// cache), so a second notification for the same post cannot survive.
	if len(page.Posts) != 1 || page.Posts[0].ID != "42" {
		t.Fatalf("posts = %+v", page.Posts)
	}
}

func TestParseNotificationsPageSkipsMalformedEntries(t *testing.T) {
	payload := decodeFixture(t, `{
	  "data": {"timeline": {"instructions": [{"entries": [
	    {"content": {"itemContent": {"__typename": "TimelineNotification"}}},
	    {"content": {"itemContent": {"notificationType": "Like", "notification": {}}}},
	    "not-even-an-object",
	    {"content": {"itemContent": {
	      "notificationType": "Mention",
	      "notification": {"message": {"text": "@eve mentioned you"},
	        "notification_url": {"url": "https://x.com/eve/status/7"}}}}}
	  ]}]}}
	}`)

	page := parseNotificationsPage(payload)
	if len(page.Posts) != 1 || page.Posts[0].ID != "7" {
		t.Fatalf("posts = %+v", page.Posts)
	}
}

func decodeFixture(t *testing.T, raw string) any {
	t.Helper()
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func configuredNotificationsTestClient(t *testing.T, handler func(*http.Request) (*http.Response, error)) *WebClient {
	t.Helper()
	client := NewWebClient(&config.Config{
		AuthToken:                "auth",
		CT0:                      "csrf",
		NotificationsTimelineQID: "notif-qid",
	})
	client.httpClient = &http.Client{Transport: roundTripFunc(handler)}
	return client
}

func decodeNotificationVariables(t *testing.T, req *http.Request) map[string]any {
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
