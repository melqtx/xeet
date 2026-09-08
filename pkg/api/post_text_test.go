package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/config"
)

func TestPostTextValidation(t *testing.T) {
	for _, tc := range []struct {
		text  string
		valid bool
	}{
		{strings.Repeat("界", LongPostLimit), true},
		{strings.Repeat("a", LongPostLimit+1), false},
		{"\xff", false},
	} {
		if err := ValidatePostText(tc.text); (err == nil) != tc.valid {
			t.Errorf("valid=%v error=%v", tc.valid, err)
		}
	}
	if PostTextLength("界\n🙂") != 3 {
		t.Fatal("counter must count code points and newlines")
	}
}

func TestLongPostReplyReusesUploadedMediaAndRefreshesNoteQuery(t *testing.T) {
	client := &WebClient{authToken: "auth", ct0: "csrf", operationQIDs: map[string]string{"CreateTweet": "short", "CreateNoteTweet": "stale"}}
	text := strings.Repeat("x", 281)
	var paths []string
	uploads := 0
	client.discover = func(_ context.Context, _, _, operation string) (string, error) {
		if operation != "CreateNoteTweet" {
			t.Fatalf("discovered %s", operation)
		}
		return "fresh", nil
	}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "media/upload") {
			uploads++
			return response(200, `{"media_id_string":"42"}`), nil
		}
		paths = append(paths, req.URL.Path)
		if strings.HasSuffix(req.URL.Path, "/CreateTweet") {
			return response(200, `{"errors":[{"code":186,"message":"Tweet needs to be a bit shorter."}]}`), nil
		}
		if strings.Contains(req.URL.Path, "/stale/") {
			return response(404, `{}`), nil
		}
		body, _ := io.ReadAll(req.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		vars := payload["variables"].(map[string]any)
		if vars["tweet_text"] != text || vars["reply"].(map[string]any)["in_reply_to_tweet_id"] != "parent" || !strings.Contains(string(body), `"media_id":"42"`) {
			t.Fatalf("lost text/reply/media: %s", body)
		}
		return response(200, `{"data":{"notetweet_create":{"tweet_results":{"result":{"tweet":{"rest_id":"99"}}}}}}`), nil
	})}
	id, err := client.PostTweet(context.Background(), text, "parent", []Upload{{Filename: "pic.png", Data: []byte("image")}}, nil)
	if err != nil || id != "99" || uploads != 1 || len(paths) != 3 {
		t.Fatalf("id=%s err=%v uploads=%d paths=%v", id, err, uploads, paths)
	}
	cfg := &config.Config{}
	if !client.ApplyRefreshedQueryIDs(cfg) || cfg.CreateNoteTweetQID != "fresh" {
		t.Fatal("note query id not persisted")
	}
}

func TestLengthFallbackRequiresDefinitiveRejection(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   bool
	}{
		{200, `{"errors":[{"code":186}]}`, true},
		{403, `{"errors":[{"code":186}]}`, true},
		{500, `{"errors":[{"code":186}]}`, false},
		{200, `{"errors":[{"code":186},{"code":88}]}`, false},
		{200, `{"data":{"create_tweet":{"tweet_results":{"result":{"rest_id":"99"}}}},"errors":[{"code":186}]}`, false},
		{200, `{"data":{"quoted":{"errors":[{"code":186}]}}}`, false},
		{200, `{}`, false},
		{200, `null`, false},
		{200, `not json`, false},
	} {
		if got := postLengthRejected(&httpResult{status: tc.status, body: []byte(tc.body)}); got != tc.want {
			t.Errorf("%d %s: got %v", tc.status, tc.body, got)
		}
	}
}

func TestLongPostFailureDoesNotRepeatMutation(t *testing.T) {
	client := &WebClient{authToken: "auth", ct0: "csrf", operationQIDs: map[string]string{"CreateTweet": "short", "CreateNoteTweet": "note"}}
	calls := 0
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(200, `{"errors":[{"code":186}]}`), nil
		}
		return response(200, `{"errors":[{"code":344,"message":"Not permitted"}]}`), nil
	})}
	_, err := client.PostTweet(context.Background(), strings.Repeat("x", 281), "", nil, nil)
	var restricted *PostingRestrictedError
	if calls != 2 || !errors.As(err, &restricted) || !strings.Contains(err.Error(), "Premium") {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestOversizePostRejectedBeforeUpload(t *testing.T) {
	client := &WebClient{authToken: "auth", ct0: "csrf"}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("oversize draft made a request"); return nil, nil })}
	if _, err := client.PostTweet(context.Background(), strings.Repeat("x", LongPostLimit+1), "", []Upload{{Data: []byte("image")}}, nil); err == nil {
		t.Fatal("oversize post accepted")
	}
}

func TestAmbiguousLongReplyNeverRetries(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":{"notetweet_create":{"tweet_results":{"result":{}}}}}`} {
		calls := 0
		client := &WebClient{authToken: "auth", ct0: "csrf", operationQIDs: map[string]string{"CreateTweet": "short", "CreateNoteTweet": "note"}}
		client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return response(200, `{"errors":[{"code":186}]}`), nil
			}
			return response(200, body), nil
		})}
		_, err := client.PostTweet(context.Background(), strings.Repeat("x", 281), "parent", nil, nil)
		var ambiguous *AmbiguousPostError
		if calls != 2 || !errors.As(err, &ambiguous) {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
}

func TestLongURLCanRemainAStandardPost(t *testing.T) {
	calls := 0
	client := &WebClient{authToken: "auth", ct0: "csrf", operationQIDs: map[string]string{"CreateTweet": "short"}}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if !strings.HasSuffix(req.URL.Path, "/CreateTweet") {
			t.Fatal("long URL was forced into Premium")
		}
		return response(200, `{"data":{"create_tweet":{"tweet_results":{"result":{"rest_id":"99"}}}}}`), nil
	})}
	id, err := client.PostTweet(context.Background(), "https://example.com/"+strings.Repeat("a", 300), "", nil, nil)
	if calls != 1 || id != "99" || err != nil {
		t.Fatalf("calls=%d id=%s err=%v", calls, id, err)
	}
}
