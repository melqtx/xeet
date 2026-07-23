package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTestClient(handler func(*http.Request) (*http.Response, error)) *WebClient {
	return &WebClient{
		authToken:  "auth",
		ct0:        "csrf",
		queryID:    "qid",
		retryDelay: time.Millisecond,
		httpClient: &http.Client{Transport: roundTripFunc(handler)},
	}
}

func TestPostTweetSessionExpired(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		client := newTestClient(func(req *http.Request) (*http.Response, error) {
			return response(status, `{"errors":[{"message":"nope"}]}`), nil
		})
		_, err := client.PostTweet(context.Background(), "hi", "", nil, nil)
		if !errors.Is(err, ErrSessionExpired) {
			t.Errorf("HTTP %d: got %v, want ErrSessionExpired", status, err)
		}
	}
}

func TestPostTweetRateLimited(t *testing.T) {
	reset := time.Now().Add(10 * time.Minute).Unix()
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		resp := response(http.StatusTooManyRequests, `{}`)
		resp.Header.Set("x-rate-limit-reset", strconv.FormatInt(reset, 10))
		return resp, nil
	})
	_, err := client.PostTweet(context.Background(), "hi", "", nil, nil)
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("got %v, want RateLimitError", err)
	}
	if rle.Reset.Unix() != reset {
		t.Errorf("reset = %v, want unix %d", rle.Reset, reset)
	}
	if !strings.Contains(rle.Error(), "min") {
		t.Errorf("error message should mention minutes: %q", rle.Error())
	}
}

func TestPostTweetGraphQLAuthError(t *testing.T) {
	// GraphQL errors arrive inside an HTTP 200. Code 32 is "could not
	// authenticate you" and must map to ErrSessionExpired.
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"errors":[{"message":"Could not authenticate you","code":32}]}`), nil
	})
	_, err := client.PostTweet(context.Background(), "hi", "", nil, nil)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("got %v, want ErrSessionExpired", err)
	}
}

func TestPostTweetDuplicateIsActionable(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"errors":[{"message":"Status is a duplicate.","code":187}]}`), nil
	})
	_, err := client.PostTweet(context.Background(), "same text", "", nil, nil)
	var recent *RecentlyPostedError
	if !errors.As(err, &recent) {
		t.Fatalf("got %v, want RecentlyPostedError", err)
	}
}

func TestPostTweetAutomationBlockIsActionable(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		// X sometimes omits code 226 and only returns the automation text.
		return response(http.StatusOK, `{"errors":[{"message":"Authorization: This request looks like it might be automated"}]}`), nil
	})
	_, err := client.PostTweet(context.Background(), "hi", "", nil, nil)
	var blocked *AutomationBlockedError
	if !errors.As(err, &blocked) || !strings.Contains(err.Error(), "try the exact text in X") {
		t.Fatalf("got %v, want automation-block guidance", err)
	}
}

func TestGraphQLErrorIgnoresNestedContentErrors(t *testing.T) {
	var payload any
	if err := json.Unmarshal([]byte(`{"data":{"operation":{"errors":[{"code":226,"message":"automated"}]}}}`), &payload); err != nil {
		t.Fatal(err)
	}
	if err := graphQLError(payload); err != nil {
		t.Fatalf("nested content error escaped: %v", err)
	}
}

func TestTimelineIgnoresNestedContentErrors(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"data":{"home":{
			"errors":[{"code":226,"message":"nested content unavailable"}],
			"instructions":[]
		}}}`), nil
	})
	page, err := client.FetchHomeTimeline(context.Background(), "", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 0 {
		t.Fatalf("posts=%d", len(page.Posts))
	}
}

func TestPostTweetNotRetriedOnTransientFailure(t *testing.T) {
	// CreateTweet is a mutation: a timed-out request may have posted anyway,
	// so it must never fire twice on its own.
	postAttempts := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			postAttempts++
			return response(http.StatusServiceUnavailable, `oops`), nil
		}
		return response(http.StatusOK, `{"data":{"home":{"instructions":[]}}}`), nil
	})
	_, err := client.PostTweet(context.Background(), "hi", "", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if postAttempts != 1 {
		t.Fatalf("CreateTweet fired %d times, want 1", postAttempts)
	}
}

func TestUploadRetriesTransientFailures(t *testing.T) {
	attempts := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "media/upload") {
			attempts++
			if attempts < 3 {
				return response(http.StatusServiceUnavailable, `flaky`), nil
			}
			return response(http.StatusOK, `{"media_id_string":"777"}`), nil
		}
		return response(http.StatusOK, `{"data":{"create_tweet":{"tweet_results":{"result":{"rest_id":"1"}}}}}`), nil
	})
	id, err := client.PostTweet(context.Background(), "pic", "",
		[]Upload{{Filename: "a.png", ContentType: "image/png", Data: []byte("x")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if id != "1" || attempts != 3 {
		t.Fatalf("id=%q attempts=%d, want id=1 attempts=3", id, attempts)
	}
}

func TestUploadGivesUpAfterMaxAttempts(t *testing.T) {
	attempts := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		attempts++
		return response(http.StatusBadGateway, `down`), nil
	})
	_, err := client.PostTweet(context.Background(), "pic", "",
		[]Upload{{Filename: "a.png", ContentType: "image/png", Data: []byte("x")}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != maxRequestAttempts {
		t.Fatalf("attempts = %d, want %d", attempts, maxRequestAttempts)
	}
}

func TestFetchHomeTimelineSessionExpired(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusUnauthorized, `{}`), nil
	})
	_, err := client.FetchHomeTimeline(context.Background(), "", 30)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("got %v, want ErrSessionExpired", err)
	}
}

func TestFetchHomeTimelineRateLimited(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusTooManyRequests, `{}`), nil
	})
	_, err := client.FetchHomeTimeline(context.Background(), "", 30)
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("got %v, want RateLimitError", err)
	}
}

func TestSetTweetLikedRetriesTransient(t *testing.T) {
	attempts := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(http.StatusServiceUnavailable, `flaky`), nil
		}
		return response(http.StatusOK, `{"data":{}}`), nil
	})
	if err := client.SetTweetLiked(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestSetTweetLikedGraphQLRateLimit(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"errors":[{"message":"Rate limit exceeded","code":88}]}`), nil
	})
	err := client.SetTweetLiked(context.Background(), "123", true)
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("got %v, want RateLimitError", err)
	}
}

func TestNeedsQueryIDRefresh(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   bool
	}{
		{http.StatusNotFound, ``, true},
		{http.StatusBadRequest, `{"errors":[{"message":"PersistedQueryNotFound"}]}`, true},
		{http.StatusBadRequest, `{"errors":[{"message":"bad queryId"}]}`, true},
		{http.StatusBadRequest, `{"errors":[{"message":"features cannot be null"}]}`, false},
		{http.StatusOK, ``, false},
		{http.StatusUnauthorized, ``, false},
	}
	for _, tc := range cases {
		res := &httpResult{status: tc.status, body: []byte(tc.body)}
		if got := needsQueryIDRefresh(res); got != tc.want {
			t.Errorf("needsQueryIDRefresh(%d, %q) = %v, want %v", tc.status, tc.body, got, tc.want)
		}
	}
}

func TestSendRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		attempts++
		cancel() // cancel while the first attempt is in flight
		return response(http.StatusServiceUnavailable, `down`), nil
	})
	client.retryDelay = time.Hour // a retry sleep would hang the test if ctx were ignored
	_, err := client.FetchHomeTimeline(ctx, "", 30)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestErrorsNeverLeakCookies(t *testing.T) {
	// Every error path must keep session material out of messages.
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, `server error`), nil
	})
	client.authToken = "tok_hunter2_secret"
	client.ct0 = "csrf_hunter2_secret"
	_, err := client.PostTweet(context.Background(), "hi", "", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, secret := range []string{"tok_hunter2_secret", "csrf_hunter2_secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error message leaks session material %q: %s", secret, err)
		}
	}
}

func TestVerifySessionExpired(t *testing.T) {
	attempts := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		attempts++
		return response(http.StatusUnauthorized, `{}`), nil
	})
	_, err := client.Verify(context.Background())
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("got %v, want ErrSessionExpired", err)
	}
	if attempts != 1 {
		t.Fatalf("verification tried %d endpoints after auth rejection, want 1", attempts)
	}
}

func TestVerifyRetriesTransientFailure(t *testing.T) {
	attempts := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(http.StatusServiceUnavailable, `temporary`), nil
		}
		return response(http.StatusOK, `{"data":{"home":{"instructions":[]}}}`), nil
	})
	handle, err := client.Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if handle != "" || attempts != 2 {
		t.Fatalf("handle=%q attempts=%d", handle, attempts)
	}
}

func TestVerifyRejectsMalformedTimeline(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{}`), nil
	})
	if _, err := client.Verify(context.Background()); err == nil {
		t.Fatal("expected malformed verification response to fail")
	}
}

func TestPostTweetRefreshesRotatedQueryID(t *testing.T) {
	attempts := 0
	var operations []string
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			if !strings.Contains(req.URL.Path, "/qid/CreateTweet") {
				t.Fatalf("first path = %s", req.URL.Path)
			}
			return response(http.StatusBadRequest, `{"errors":[{"message":"PersistedQueryNotFound"}]}`), nil
		}
		if !strings.Contains(req.URL.Path, "/fresh/CreateTweet") {
			t.Fatalf("retry path = %s", req.URL.Path)
		}
		return response(http.StatusOK, `{"data":{"create_tweet":{"tweet_results":{"result":{"rest_id":"99"}}}}}`), nil
	})
	client.discover = func(ctx context.Context, auth, ct0, operation string) (string, error) {
		operations = append(operations, operation)
		if auth != "auth" || ct0 != "csrf" {
			t.Fatal("discovery did not receive session credentials")
		}
		return "fresh", nil
	}

	id, err := client.PostTweet(context.Background(), "hello", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if id != "99" || attempts != 2 || len(operations) != 1 || operations[0] != "CreateTweet" {
		t.Fatalf("id=%q attempts=%d operations=%v", id, attempts, operations)
	}
	if !client.Refreshed() || client.QueryID() != "fresh" {
		t.Fatalf("refreshed=%v qid=%q", client.Refreshed(), client.QueryID())
	}
}

func TestPostTweetRejectsMissingCreatedID(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"data":{"create_tweet":{"tweet_results":{"result":{}}}}}`), nil
	})
	if _, err := client.PostTweet(context.Background(), "hello", "", nil, nil); err == nil {
		t.Fatal("missing created post id was reported as success")
	}
}

func TestTimelineRefreshesRotatedQueryID(t *testing.T) {
	attempts := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(http.StatusNotFound, `{}`), nil
		}
		if !strings.Contains(req.URL.Path, "/fresh/HomeTimeline") {
			t.Fatalf("retry path = %s", req.URL.Path)
		}
		return response(http.StatusOK, `{"data":{"home":{"instructions":[]}}}`), nil
	})
	client.discover = func(context.Context, string, string, string) (string, error) { return "fresh", nil }
	page, err := client.FetchHomeTimeline(context.Background(), "", 30)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || len(page.Posts) != 0 {
		t.Fatalf("attempts=%d page=%+v", attempts, page)
	}
}

func TestLikeRefreshesRotatedQueryID(t *testing.T) {
	attempts := 0
	operation := ""
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(http.StatusNotFound, `{}`), nil
		}
		if !strings.Contains(req.URL.Path, "/fresh/FavoriteTweet") {
			t.Fatalf("retry path = %s", req.URL.Path)
		}
		return response(http.StatusOK, `{"data":{"favorite_tweet":"Done"}}`), nil
	})
	client.discover = func(ctx context.Context, auth, ct0, op string) (string, error) {
		operation = op
		return "fresh", nil
	}
	if err := client.SetTweetLiked(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || operation != "FavoriteTweet" {
		t.Fatalf("attempts=%d operation=%q", attempts, operation)
	}
}

func TestTimelineRejectsMalformedPayload(t *testing.T) {
	for _, body := range []string{`[]`, `{}`, `{"not_data":{}}`} {
		client := newTestClient(func(req *http.Request) (*http.Response, error) {
			return response(http.StatusOK, body), nil
		})
		if _, err := client.FetchHomeTimeline(context.Background(), "", 30); err == nil {
			t.Errorf("payload %s was accepted", body)
		}
	}
}

func TestTimelineAllowsValidEmptyPage(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"data":{"home":{"instructions":[]}}}`), nil
	})
	page, err := client.FetchHomeTimeline(context.Background(), "", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 0 {
		t.Fatalf("posts = %d, want 0", len(page.Posts))
	}
}
