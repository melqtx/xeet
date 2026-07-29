package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/config"
)

func configuredRetweetTestClient(t *testing.T, cfg *config.Config, handler func(*http.Request) (*http.Response, error)) *WebClient {
	t.Helper()
	cfg.AuthToken, cfg.CT0 = "auth", "csrf"
	client := NewWebClient(cfg)
	client.httpClient = &http.Client{Transport: roundTripFunc(handler)}
	// The generator scrapes X's transaction page; tests stub the minting step
	// and assert on the header it produces instead.
	client.transactionID = func(context.Context, string, string) (string, error) {
		return "tx-id", nil
	}
	return client
}

func TestSetTweetRepostedPostsTweetIDToCreateRetweet(t *testing.T) {
	var path, body string
	client := configuredRetweetTestClient(t, &config.Config{CreateRetweetQID: "rt-qid"}, func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return response(http.StatusOK, `{"data":{"create_retweet":{"retweet_results":{}}}}`), nil
	})

	if err := client.SetTweetReposted(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "/rt-qid/CreateRetweet") || !strings.Contains(body, `"tweet_id":"123"`) {
		t.Fatalf("path=%q body=%s", path, body)
	}
}

func TestSetTweetRepostedSendsTransactionHeader(t *testing.T) {
	var header string
	client := configuredRetweetTestClient(t, &config.Config{CreateRetweetQID: "rt-qid"}, func(req *http.Request) (*http.Response, error) {
		header = req.Header.Get("X-Client-Transaction-Id")
		return response(http.StatusOK, `{"data":{"create_retweet":{"retweet_results":{}}}}`), nil
	})

	if err := client.SetTweetReposted(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if header != "tx-id" {
		t.Fatalf("X-Client-Transaction-Id = %q, want tx-id", header)
	}
}

func TestSetTweetUnrepostedPostsSourceTweetIDToDeleteRetweet(t *testing.T) {
	var path, body string
	client := configuredRetweetTestClient(t, &config.Config{DeleteRetweetQID: "drt-qid"}, func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return response(http.StatusOK, `{"data":{"unretweet":{}}}`), nil
	})

	if err := client.SetTweetReposted(context.Background(), "123", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "/drt-qid/DeleteRetweet") || !strings.Contains(body, `"source_tweet_id":"123"`) {
		t.Fatalf("path=%q body=%s", path, body)
	}
}

func TestSetTweetRepostedDiscoversMissingQueryIDBeforeFirstRequestAndExposesItForPersistence(t *testing.T) {
	var path string
	client := configuredRetweetTestClient(t, &config.Config{}, func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		return response(http.StatusOK, `{"data":{"create_retweet":{"retweet_results":{}}}}`), nil
	})
	client.discover = func(_ context.Context, auth, ct0, operation string) (string, error) {
		if auth != "auth" || ct0 != "csrf" || operation != createRetweetOperation {
			t.Fatalf("discovery args = auth:%q ct0:%q operation:%q", auth, ct0, operation)
		}
		return "fresh-rt", nil
	}

	if err := client.SetTweetReposted(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "/fresh-rt/CreateRetweet") {
		t.Fatalf("path=%q", path)
	}
	cfg := &config.Config{}
	if !client.ApplyRefreshedQueryIDs(cfg) || cfg.CreateRetweetQID != "fresh-rt" {
		t.Fatalf("config=%+v", cfg)
	}
}

func TestSetTweetRepostedRotatesQueryIDOnceOnPersistedQueryMiss(t *testing.T) {
	var paths []string
	client := configuredRetweetTestClient(t, &config.Config{CreateRetweetQID: "stale"}, func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if len(paths) == 1 {
			return response(http.StatusNotFound, ``), nil
		}
		return response(http.StatusOK, `{"data":{"create_retweet":{"retweet_results":{}}}}`), nil
	})
	discoveries := 0
	client.discover = func(_ context.Context, _, _, operation string) (string, error) {
		discoveries++
		return "fresh-rt", nil
	}

	if err := client.SetTweetReposted(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if discoveries != 1 || len(paths) != 2 ||
		!strings.Contains(paths[0], "/stale/CreateRetweet") || !strings.Contains(paths[1], "/fresh-rt/CreateRetweet") {
		t.Fatalf("discoveries=%d paths=%v", discoveries, paths)
	}
}

// Retweets are not idempotent — a replayed request errors as "already
// retweeted" — so a transient 503 must surface after exactly one attempt and
// leave the retry to the user, unlike the like path's transparent retry.
func TestSetTweetRepostedDoesNotRetryTransientStatus(t *testing.T) {
	attempts := 0
	client := configuredRetweetTestClient(t, &config.Config{CreateRetweetQID: "rt-qid"}, func(req *http.Request) (*http.Response, error) {
		attempts++
		return response(http.StatusServiceUnavailable, ``), nil
	})

	err := client.SetTweetReposted(context.Background(), "123", true)
	var unavailable *ServiceUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("err = %v, want ServiceUnavailableError", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (no transient retry for retweets)", attempts)
	}
}

func TestSetTweetRepostedHonorsEnvironmentQueryIDOverride(t *testing.T) {
	t.Setenv("XEET_CREATERETWEET_QID", "env-rt")
	var path string
	client := configuredRetweetTestClient(t, &config.Config{CreateRetweetQID: "cfg-rt"}, func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		return response(http.StatusOK, `{"data":{"create_retweet":{"retweet_results":{}}}}`), nil
	})

	if err := client.SetTweetReposted(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "/env-rt/CreateRetweet") {
		t.Fatalf("path=%q", path)
	}
}

func TestSetTweetRepostedRejectsMissingSessionAndEmptyIDBeforeAnyRequest(t *testing.T) {
	requests := 0
	client := configuredRetweetTestClient(t, &config.Config{CreateRetweetQID: "rt-qid"}, func(req *http.Request) (*http.Response, error) {
		requests++
		return response(http.StatusOK, `{}`), nil
	})
	client.authToken, client.ct0 = "", ""

	if err := client.SetTweetReposted(context.Background(), "123", true); err == nil {
		t.Fatal("SetTweetReposted succeeded without a session")
	}
	client.authToken, client.ct0 = "auth", "csrf"
	if err := client.SetTweetReposted(context.Background(), "", true); err == nil {
		t.Fatal("SetTweetReposted succeeded with an empty tweet id")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}
