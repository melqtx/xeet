package timeline

import (
	"net/url"
	"testing"
)

func TestReplyIntentURLPreservesTargetAndText(t *testing.T) {
	target := replyIntentURL("12345", "no & maybe")
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "x.com" || parsed.Path != "/intent/tweet" {
		t.Fatalf("target=%q", target)
	}
	if got := parsed.Query().Get("in_reply_to"); got != "12345" {
		t.Fatalf("in_reply_to=%q", got)
	}
	if got := parsed.Query().Get("text"); got != "no & maybe" {
		t.Fatalf("text=%q", got)
	}
}
