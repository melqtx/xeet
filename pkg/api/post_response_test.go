package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCreateTweetResponseAcceptsVisibilityWrapper(t *testing.T) {
	res := &httpResult{
		status: http.StatusOK,
		header: make(http.Header),
		body: []byte(`{"data":{"create_tweet":{"tweet_results":{"result":{
			"__typename":"TweetWithVisibilityResults","tweet":{"rest_id":"12345"}
		}}}}}`),
	}
	outcome := parseCreateTweetResponse(res)
	if outcome.err != nil || outcome.id != "12345" {
		t.Fatalf("outcome=%+v", outcome)
	}
	if !strings.Contains(outcome.diagnostic, "id_path=data.create_tweet.tweet_results.result.tweet.rest_id") {
		t.Fatalf("diagnostic=%q", outcome.diagnostic)
	}
}

func TestCreateTweetResponseFindsNestedAutomationError(t *testing.T) {
	res := &httpResult{
		status: http.StatusOK,
		header: make(http.Header),
		body: []byte(`{"data":{"create_tweet":{"errors":[{
			"code":226,"message":"Authorization: This request looks like it might be automated"
		}]}}}`),
	}
	outcome := parseCreateTweetResponse(res)
	var blocked *AutomationBlockedError
	if !errors.As(outcome.err, &blocked) {
		t.Fatalf("error=%v, want AutomationBlockedError", outcome.err)
	}
	if !strings.Contains(outcome.diagnostic, "errors=226") {
		t.Fatalf("diagnostic=%q", outcome.diagnostic)
	}
}

func TestCreateTweetResponseSkipsEmptyErrorEntries(t *testing.T) {
	res := &httpResult{
		status: http.StatusOK,
		header: make(http.Header),
		body:   []byte(`{"errors":[null,{},{"code":344,"message":"posting restricted"}]}`),
	}
	outcome := parseCreateTweetResponse(res)
	var restricted *PostingRestrictedError
	if !errors.As(outcome.err, &restricted) {
		t.Fatalf("error=%v, want PostingRestrictedError", outcome.err)
	}
	if !strings.Contains(outcome.diagnostic, "errors=344") {
		t.Fatalf("diagnostic=%q", outcome.diagnostic)
	}
}

func TestCreateTweetResponseCarriesRateReset(t *testing.T) {
	reset := time.Now().Add(5 * time.Minute).Unix()
	header := make(http.Header)
	header.Set("x-rate-limit-reset", fmt.Sprint(reset))
	res := &httpResult{
		status: http.StatusOK,
		header: header,
		body:   []byte(`{"errors":[{"code":88,"message":"Rate limit exceeded"}]}`),
	}
	outcome := parseCreateTweetResponse(res)
	var rateLimit *RateLimitError
	if !errors.As(outcome.err, &rateLimit) || rateLimit.Reset.Unix() != reset {
		t.Fatalf("error=%v reset=%v", outcome.err, rateLimit)
	}
	if !strings.Contains(outcome.diagnostic, "reset=") {
		t.Fatalf("diagnostic=%q", outcome.diagnostic)
	}
}

func TestPostingRestrictionOmitsIrrelevantReset(t *testing.T) {
	header := make(http.Header)
	header.Set("x-rate-limit-reset", "1784819181")
	res := &httpResult{
		status: http.StatusOK,
		header: header,
		body:   []byte(`{"errors":[{"code":344,"message":"daily limit"}]}`),
	}
	outcome := parseCreateTweetResponse(res)
	var restricted *PostingRestrictedError
	if !errors.As(outcome.err, &restricted) {
		t.Fatalf("error=%v, want PostingRestrictedError", outcome.err)
	}
	if strings.Contains(outcome.diagnostic, "reset=") {
		t.Fatalf("diagnostic contains irrelevant reset: %q", outcome.diagnostic)
	}
}

func TestCreateTweetDiagnosticNeverContainsPostTextOrErrorMessage(t *testing.T) {
	const secret = "private draft words must not leak"
	res := &httpResult{
		status: http.StatusOK,
		header: make(http.Header),
		body: []byte(`{"data":{"create_tweet":{"tweet_results":{"result":{
			"legacy":{"full_text":"` + secret + `"}
		}}}}}`),
	}
	outcome := parseCreateTweetResponse(res)
	if !outcome.ambiguous {
		t.Fatalf("outcome=%+v", outcome)
	}
	if strings.Contains(outcome.diagnostic, secret) || strings.Contains(outcome.diagnostic, "full_text") {
		t.Fatalf("diagnostic leaked response content: %q", outcome.diagnostic)
	}
	if strings.Contains(outcome.err.Error(), secret) {
		t.Fatalf("error leaked response content: %q", outcome.err)
	}
	if ErrorDiagnostic(outcome.err) != outcome.diagnostic {
		t.Fatalf("diagnostic was not attached to error")
	}
}

func TestAmbiguousCreateReconcilesWithOneReadAndNoMutationRetry(t *testing.T) {
	created := time.Now().Add(-time.Second).Format("Mon Jan 02 15:04:05 -0700 2006")
	postCalls, readCalls := 0, 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodPost:
			postCalls++
			return response(http.StatusOK, `{"data":{"create_tweet":{"tweet_results":{"result":{}}}}}`), nil
		case http.MethodGet:
			readCalls++
			body := fmt.Sprintf(`{"data":{"home":{"instructions":[{"entries":[{
				"content":{"itemContent":{"tweet_results":{"result":{
					"rest_id":"found-id",
					"legacy":{"full_text":"hello from reconciliation","created_at":%q}
				}}}}
			}] }]}}}`, created)
			return response(http.StatusOK, body), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			return nil, nil
		}
	})

	var events []PostStage
	id, err := client.PostTweet(context.Background(), "hello from reconciliation", "", "", nil, func(event PostEvent) {
		events = append(events, event.Stage)
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "found-id" || postCalls != 1 || readCalls != 1 {
		t.Fatalf("id=%q postCalls=%d readCalls=%d", id, postCalls, readCalls)
	}
	if !containsStage(events, PostStageReconciling) || events[len(events)-1] != PostStageComplete {
		t.Fatalf("events=%v", events)
	}
}

func TestAmbiguousCreateRemainsAmbiguousWhenReadCannotConfirm(t *testing.T) {
	postCalls := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			postCalls++
			return response(http.StatusOK, `{"data":{"create_tweet":{"tweet_results":{"result":{}}}}}`), nil
		}
		return response(http.StatusOK, `{"data":{"home":{"instructions":[]}}}`), nil
	})

	_, err := client.PostTweet(context.Background(), "not in timeline", "", "", nil, nil)
	var ambiguous *AmbiguousPostError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("error=%v, want AmbiguousPostError", err)
	}
	if postCalls != 1 {
		t.Fatalf("CreateTweet fired %d times", postCalls)
	}
	if diagnostic := ErrorDiagnostic(err); !strings.Contains(diagnostic, "reconcile=not_found") {
		t.Fatalf("diagnostic=%q", diagnostic)
	}
}

func TestTransportFailureReconcilesWithoutMutationRetry(t *testing.T) {
	created := time.Now().Add(-time.Second).Format("Mon Jan 02 15:04:05 -0700 2006")
	postCalls, readCalls := 0, 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			postCalls++
			return nil, errors.New("connection reset after write")
		}
		readCalls++
		body := fmt.Sprintf(`{"data":{"home":{"instructions":[{"entries":[{
			"content":{"itemContent":{"tweet_results":{"result":{
				"rest_id":"transport-found",
				"legacy":{"full_text":"possibly landed","created_at":%q}
			}}}}
		}] }]}}}`, created)
		return response(http.StatusOK, body), nil
	})

	id, err := client.PostTweet(context.Background(), "possibly landed", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if id != "transport-found" || postCalls != 1 || readCalls != 1 {
		t.Fatalf("id=%q postCalls=%d readCalls=%d", id, postCalls, readCalls)
	}
	if diagnostic := client.LastDiagnostic(); diagnostic != "transport=failed reconcile=found" {
		t.Fatalf("diagnostic=%q", diagnostic)
	}
}

func TestMalformedSuccessResponseIsAmbiguous(t *testing.T) {
	postCalls := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			postCalls++
			return response(http.StatusOK, `not-json`), nil
		}
		return response(http.StatusOK, `{"data":{"home":{"instructions":[]}}}`), nil
	})

	_, err := client.PostTweet(context.Background(), "maybe", "", "", nil, nil)
	var ambiguous *AmbiguousPostError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("error=%v, want AmbiguousPostError", err)
	}
	if postCalls != 1 {
		t.Fatalf("CreateTweet fired %d times", postCalls)
	}
}

func containsStage(events []PostStage, want PostStage) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}
