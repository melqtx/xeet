package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSetTweetLiked(t *testing.T) {
	var path, body string
	client := &WebClient{authToken: "auth", ct0: "csrf"}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return response(http.StatusOK, `{"data":{"favorite_tweet":"Done"}}`), nil
	})}
	if err := client.SetTweetLiked(context.Background(), "123", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "/FavoriteTweet") || !strings.Contains(body, `"tweet_id":"123"`) {
		t.Fatalf("path=%q body=%s", path, body)
	}
}

func TestSetTweetUnliked(t *testing.T) {
	var path string
	client := &WebClient{authToken: "auth", ct0: "csrf"}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		return response(http.StatusOK, `{"data":{"unfavorite_tweet":"Done"}}`), nil
	})}
	if err := client.SetTweetLiked(context.Background(), "123", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "/UnfavoriteTweet") {
		t.Fatalf("path=%q", path)
	}
}
