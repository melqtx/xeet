package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNeedsChunkedUpload(t *testing.T) {
	cases := []struct {
		upload Upload
		want   bool
	}{
		{Upload{ContentType: "image/png", Data: []byte("x")}, false},
		{Upload{ContentType: "video/mp4", Path: "/tmp/a.mp4"}, true},
		{Upload{ContentType: "video/mp4", Data: []byte("x")}, true},
		{Upload{ContentType: "image/gif", Data: make([]byte, chunkedUploadThreshold+1)}, true},
	}
	for _, c := range cases {
		if got := c.upload.needsChunkedUpload(); got != c.want {
			t.Errorf("needsChunkedUpload(%q path=%q len=%d) = %v, want %v",
				c.upload.ContentType, c.upload.Path, len(c.upload.Data), got, c.want)
		}
	}
}

func TestMediaCategory(t *testing.T) {
	cases := map[string]string{
		"video/mp4":       "tweet_video",
		"video/quicktime": "tweet_video",
		"image/gif":       "tweet_gif",
		"image/png":       "tweet_image",
	}
	for contentType, want := range cases {
		if got := mediaCategory(contentType); got != want {
			t.Errorf("mediaCategory(%q) = %q, want %q", contentType, got, want)
		}
	}
}

// TestPostTweetChunkedVideo walks the full INIT/APPEND/FINALIZE/STATUS flow
// for a file-backed video large enough to need two segments, then confirms
// the media id lands in CreateTweet.
func TestPostTweetChunkedVideo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	payload := bytes.Repeat([]byte("v"), uploadChunkSize+512)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	var commands []string
	var appended int
	statusPolls := 0
	var graphQLBody []byte
	client := &WebClient{authToken: "auth", ct0: "csrf", queryID: "qid"}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.Body != nil {
			body, _ = io.ReadAll(req.Body)
		}
		if !strings.Contains(req.URL.Path, "media/upload") {
			graphQLBody = body
			return response(http.StatusOK, `{"data":{"create_tweet":{"tweet_results":{"result":{"rest_id":"77"}}}}}`), nil
		}
		switch {
		case req.Method == http.MethodGet:
			commands = append(commands, "STATUS")
			statusPolls++
			state := `"in_progress","check_after_secs":0`
			if statusPolls >= 2 {
				state = `"succeeded"`
			}
			return response(http.StatusOK, `{"media_id_string":"555","processing_info":{"state":`+state+`}}`), nil
		case strings.Contains(string(body), "INIT"):
			commands = append(commands, "INIT")
			text := string(body)
			if !strings.Contains(text, "tweet_video") || !strings.Contains(text, "video%2Fmp4") {
				t.Errorf("INIT missing category or media type: %s", text)
			}
			return response(http.StatusAccepted, `{"media_id_string":"555"}`), nil
		case strings.Contains(string(body), "APPEND"):
			commands = append(commands, "APPEND")
			appended += len(body)
			return response(http.StatusNoContent, ""), nil
		case strings.Contains(string(body), "FINALIZE"):
			commands = append(commands, "FINALIZE")
			return response(http.StatusOK, `{"media_id_string":"555","processing_info":{"state":"pending","check_after_secs":0}}`), nil
		}
		t.Errorf("unexpected upload request: %s", body)
		return response(http.StatusBadRequest, ""), nil
	})}

	var events []PostEvent
	id, err := client.PostTweet(context.Background(), "clip", "", []Upload{
		{Filename: "clip.mp4", ContentType: "video/mp4", Path: path},
	}, func(event PostEvent) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if id != "77" {
		t.Fatalf("id = %q", id)
	}
	sequence := strings.Join(commands, " ")
	if !strings.HasPrefix(sequence, "INIT APPEND APPEND FINALIZE STATUS") {
		t.Fatalf("unexpected upload sequence: %s", sequence)
	}
	if appended < len(payload) {
		t.Fatalf("segments carried %d bytes, want at least %d", appended, len(payload))
	}
	if !strings.Contains(string(graphQLBody), `"media_id":"555"`) {
		t.Fatalf("CreateTweet missing media id: %s", graphQLBody)
	}
	var sawBytes, sawProcessing bool
	for _, event := range events {
		if event.Stage == PostStageUploading && event.TotalBytes == int64(len(payload)) && event.TransferredBytes > 0 {
			sawBytes = true
		}
		if event.Stage == PostStageProcessing {
			sawProcessing = true
		}
	}
	if !sawBytes || !sawProcessing {
		t.Fatalf("missing byte progress or processing events: %+v", events)
	}
}

func TestChunkedUploadProcessingFailure(t *testing.T) {
	client := &WebClient{authToken: "auth", ct0: "csrf"}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		switch {
		case strings.Contains(string(body), "INIT"):
			return response(http.StatusOK, `{"media_id_string":"9"}`), nil
		case strings.Contains(string(body), "APPEND"):
			return response(http.StatusNoContent, ""), nil
		default:
			return response(http.StatusOK, `{"media_id_string":"9","processing_info":{"state":"failed","error":{"message":"InvalidMedia: unsupported codec"}}}`), nil
		}
	})}
	_, err := client.uploadMediaChunked(context.Background(), Upload{
		Filename: "bad.mp4", ContentType: "video/mp4", Data: []byte("not really video"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported codec") {
		t.Fatalf("expected processing failure with server message, got %v", err)
	}
}
