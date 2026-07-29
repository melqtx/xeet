package api

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

// A quote only counts as working when the posted tweet carries quoted_status;
// CreateTweet returns 200 with the text alone if the attachment_url shape is
// wrong, which a mock can never catch. The posted quote is left for manual
// deletion (there is no delete API in scope).
func TestQuoteLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_QUOTE") != "1" {
		t.Skip("set XEET_LIVE_QUOTE=1 to run")
	}
	quoteID := os.Getenv("XEET_LIVE_QUOTE_TWEET_ID")
	if quoteID == "" {
		t.Skip("set XEET_LIVE_QUOTE_TWEET_ID to a post worth quoting")
	}
	mgr, err := config.NewConfigManager()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := NewWebClient(cfg)

	id, err := client.PostTweet(ctx, "xeet quote live test (delete me)", "", quoteID, nil, nil)
	if err != nil {
		t.Fatalf("PostTweet with quote: %v", err)
	}
	if id == "" {
		t.Fatal("CreateTweet returned no id")
	}
	t.Logf("posted quote %s of %s — delete it by hand", id, quoteID)

	page, err := client.FetchTweetDetail(ctx, id, "", 5)
	if err != nil {
		t.Fatalf("read back the quote: %v", err)
	}
	if len(page.Posts) == 0 || page.Posts[0].ID != id {
		t.Fatalf("posted quote missing from its detail page: %+v", page.Posts)
	}
	// The quoted post rides inside the tweet's own payload as
	// legacy.quoted_status_id_str, which the parser deliberately does not
	// surface — so the check scans the raw detail body. If the attachment_url
	// shape were wrong the text would post with nothing quoted, and this scan
	// is the only thing that catches it.
	qid := client.operationQueryID("TweetDetail", "", "XEET_TWEETDETAIL_QID")
	if qid == "" {
		fresh, derr := client.discoverOperation(ctx, "TweetDetail")
		if derr != nil {
			t.Fatalf("discover TweetDetail: %v", derr)
		}
		qid = fresh
	}
	res, err := client.doTweetDetail(ctx, qid, id, "", 5)
	if err != nil {
		t.Fatalf("raw detail fetch: %v", err)
	}
	if !strings.Contains(string(res.body), `"quoted_status_id_str":"`+quoteID+`"`) {
		t.Fatalf("posted tweet does not quote %s; attachment_url shape is wrong", quoteID)
	}
}
