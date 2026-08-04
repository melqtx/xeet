package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/melqtx/xeet/pkg/config"
)

const notificationsFixture = `{
  "data": {"viewer_v2": {"user_results": {"result": {
    "rest_id": "viewer-1",
    "notification_timeline": {"timeline": {"instructions": [{
      "type": "TimelineAddEntries",
      "entries": [
        {"entryId":"tweet-reply","content":{"itemContent":{
          "itemType":"TimelineTweet",
          "tweet_results":{"result":{
            "rest_id":"200",
            "legacy":{"full_text":"@me hello","in_reply_to_status_id_str":"100"},
            "core":{"user_results":{"result":{"core":{"name":"Alice","screen_name":"alice"}}}}
          }}
        }}},
        {"entryId":"tweet-mention","content":{"itemContent":{
          "itemType":"TimelineTweet",
          "tweet_results":{"result":{
            "rest_id":"190",
            "legacy":{"full_text":"hello @me"},
            "core":{"user_results":{"result":{"core":{"name":"Bob","screen_name":"bob"}}}}
          }}
        }}},
        {"entryId":"like","content":{"itemContent":{
          "itemType":"TimelineNotification",
          "template":{"target_objects":[{"tweet_results":{"result":{"rest_id":"own-post","legacy":{"full_text":"mine"}}}}]}
        }}},
        {"entryId":"cursor-bottom","content":{"cursorType":"Bottom","value":"next-page"}}
      ]
    }]}}
  }}}}
}`

func TestParseNotificationsKeepsActionableTweets(t *testing.T) {
	var payload any
	if err := json.Unmarshal([]byte(notificationsFixture), &payload); err != nil {
		t.Fatal(err)
	}
	page := parseNotifications(payload)
	if page.AccountID != "viewer-1" || page.Cursor != "next-page" || len(page.Notifications) != 2 {
		t.Fatalf("page = %+v", page)
	}
	if page.Notifications[0].Kind != NotificationReply || page.Notifications[0].Post.Handle != "alice" {
		t.Fatalf("reply = %+v", page.Notifications[0])
	}
	if page.Notifications[1].Kind != NotificationMention || page.Notifications[1].Post.Handle != "bob" {
		t.Fatalf("mention = %+v", page.Notifications[1])
	}
}

func TestFetchNotificationsRequestShape(t *testing.T) {
	var requestURL *url.URL
	var transaction string
	client := &WebClient{
		authToken: "auth", ct0: "csrf",
		operationQIDs: map[string]string{notificationsOperation: "notifications-qid"},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestURL = req.URL
			transaction = req.Header.Get("X-Client-Transaction-Id")
			return response(http.StatusOK, notificationsFixture), nil
		})},
	}
	client.transactionID = func(context.Context, string, string) (string, error) { return "transaction-id", nil }
	page, err := client.FetchNotifications(context.Background(), "cursor-1", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Notifications) != 2 || requestURL == nil || requestURL.Path != "/i/api/graphql/notifications-qid/NotificationsTimeline" {
		t.Fatalf("page=%+v url=%v", page, requestURL)
	}
	var variables map[string]any
	if err := json.Unmarshal([]byte(requestURL.Query().Get("variables")), &variables); err != nil {
		t.Fatal(err)
	}
	if variables["timeline_type"] != "All" || variables["cursor"] != "cursor-1" || variables["count"] != float64(25) {
		t.Fatalf("variables = %+v", variables)
	}
	if transaction != "transaction-id" {
		t.Fatalf("transaction header = %q", transaction)
	}
}

func TestNotificationsMapsDataNullAuthError(t *testing.T) {
	client := &WebClient{
		authToken: "auth", ct0: "csrf",
		operationQIDs: map[string]string{notificationsOperation: "notifications-qid"},
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, `{"data":null,"errors":[{"message":"Could not authenticate you","code":32}]}`), nil
		})},
	}
	client.transactionID = func(context.Context, string, string) (string, error) { return "transaction-id", nil }
	if _, err := client.FetchNotifications(context.Background(), "", 20); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("got %v, want ErrSessionExpired", err)
	}
}

func TestNotificationsRefreshesStaleQueryIDFromGraphQLError(t *testing.T) {
	calls := 0
	client := &WebClient{
		authToken: "auth", ct0: "csrf",
		operationQIDs: map[string]string{notificationsOperation: "stale"},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return response(http.StatusOK, `{"errors":[{"message":"PersistedQueryNotFound"}]}`), nil
			}
			return response(http.StatusOK, notificationsFixture), nil
		})},
	}
	client.transactionID = func(context.Context, string, string) (string, error) { return "transaction-id", nil }
	client.discover = func(context.Context, string, string, string) (string, error) { return "fresh", nil }
	if _, err := client.FetchNotifications(context.Background(), "", 20); err != nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestNotificationsRefreshesStaleQueryID(t *testing.T) {
	calls := 0
	client := &WebClient{
		authToken: "auth", ct0: "csrf",
		operationQIDs: map[string]string{notificationsOperation: "stale"},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return response(http.StatusNotFound, ""), nil
			}
			return response(http.StatusOK, notificationsFixture), nil
		})},
	}
	client.transactionID = func(context.Context, string, string) (string, error) { return "transaction-id", nil }
	client.discover = func(_ context.Context, _, _ string, operation string) (string, error) {
		if operation != notificationsOperation {
			t.Fatalf("operation = %q", operation)
		}
		return "fresh", nil
	}
	if _, err := client.FetchNotifications(context.Background(), "", 20); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	if calls != 2 || !client.ApplyRefreshedQueryIDs(cfg) || cfg.NotificationsQID != "fresh" {
		t.Fatalf("calls=%d cfg=%+v", calls, cfg)
	}
}
