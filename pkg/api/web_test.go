package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestChromiumFingerprintMatchesPlatform(t *testing.T) {
	if got := chromiumUserAgent("darwin"); !strings.Contains(got, "Macintosh") || chromiumClientPlatform("darwin") != `"macOS"` {
		t.Fatalf("macOS fingerprint = %q / %q", got, chromiumClientPlatform("darwin"))
	}
	if got := chromiumUserAgent("linux"); !strings.Contains(got, "X11; Linux") || chromiumClientPlatform("linux") != `"Linux"` {
		t.Fatalf("Linux fingerprint = %q / %q", got, chromiumClientPlatform("linux"))
	}
}

func TestCreateTweetPayloadShape(t *testing.T) {
	vars := createTweetVariables{
		TweetText:             "hello",
		Media:                 ctMedia{MediaEntities: []mediaEntity{}, PossiblySensitive: false},
		SemanticAnnotationIDs: []string{},
	}
	payload := map[string]any{
		"variables": vars,
		"features":  createTweetFeatures,
		"queryId":   "abc",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"tweet_text":"hello"`, `"semantic_annotation_ids":[]`, `"media_entities":[]`, `"queryId":"abc"`} {
		if !strings.Contains(s, want) {
			t.Errorf("payload missing %s\ngot: %s", want, s)
		}
	}
	// No reply field when not replying.
	if strings.Contains(s, "in_reply_to_tweet_id") {
		t.Errorf("unexpected reply field in non-reply payload: %s", s)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestPostTweetUploadsMultipleImages(t *testing.T) {
	uploadCount := 0
	var graphQLBody []byte
	client := &WebClient{authToken: "auth", ct0: "csrf", queryID: "qid"}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		if strings.Contains(req.URL.Path, "media/upload") {
			uploadCount++
			if !strings.Contains(req.Header.Get("Content-Type"), "multipart/form-data") || !strings.Contains(string(body), "image/png") {
				t.Errorf("upload metadata missing: content-type=%q body=%q", req.Header.Get("Content-Type"), body)
			}
			return response(http.StatusOK, `{"media_id_string":"`+string(rune('0'+uploadCount))+`"}`), nil
		}
		graphQLBody = body
		return response(http.StatusOK, `{"data":{"create_tweet":{"tweet_results":{"result":{"rest_id":"99"}}}}}`), nil
	})}

	var events []PostEvent
	id, err := client.PostTweet(context.Background(), "album", "", []Upload{
		{Filename: "one.png", ContentType: "image/png", Data: []byte("one")},
		{Filename: "two.png", ContentType: "image/png", Data: []byte("two")},
	}, func(event PostEvent) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if id != "99" || uploadCount != 2 {
		t.Fatalf("id=%q uploads=%d", id, uploadCount)
	}
	if strings.Count(string(graphQLBody), `"media_id"`) != 2 {
		t.Fatalf("expected two media entities: %s", graphQLBody)
	}
	if len(events) != 4 || events[0].Stage != PostStageUploading || events[len(events)-1].Stage != PostStageComplete {
		t.Fatalf("unexpected progress events: %+v", events)
	}
}

func TestPostTweetValidatesContent(t *testing.T) {
	client := &WebClient{authToken: "auth", ct0: "csrf"}
	if _, err := client.PostTweet(context.Background(), "", "", nil, nil); err == nil {
		t.Fatal("empty post should fail")
	}
	uploads := make([]Upload, 5)
	for i := range uploads {
		uploads[i] = Upload{Data: []byte("x")}
	}
	if _, err := client.PostTweet(context.Background(), "x", "", uploads, nil); err == nil {
		t.Fatal("five images should fail")
	}
}

func TestCreateTweetReplyPayload(t *testing.T) {
	vars := createTweetVariables{
		TweetText:             "re",
		Media:                 ctMedia{MediaEntities: []mediaEntity{}},
		SemanticAnnotationIDs: []string{},
		Reply:                 &ctReply{InReplyToTweetID: "123", ExcludeReplyUserIDs: []string{}},
	}
	b, _ := json.Marshal(vars)
	if !strings.Contains(string(b), `"in_reply_to_tweet_id":"123"`) {
		t.Errorf("reply id missing: %s", string(b))
	}
}
