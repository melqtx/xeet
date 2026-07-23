package tui

import (
	"net/url"
	"testing"
)

func TestPostIntentURLPreservesDraft(t *testing.T) {
	target := postIntentURL("hello & tea\nsecond line")
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "x.com" || parsed.Path != "/intent/tweet" {
		t.Fatalf("target=%q", target)
	}
	if got := parsed.Query().Get("text"); got != "hello & tea\nsecond line" {
		t.Fatalf("text=%q", got)
	}
}
