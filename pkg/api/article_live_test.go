package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

// Article parsing rests on X's response shape (article.article_results.result
// .plain_text), which a fixture can only guess at. Given a real article post
// id, this proves both the withArticlePlainText opt-in and the field path.
func TestFetchArticleLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_ARTICLE") != "1" {
		t.Skip("set XEET_LIVE_ARTICLE=1 to run")
	}
	tweetID := os.Getenv("XEET_LIVE_ARTICLE_TWEET_ID")
	if tweetID == "" {
		t.Skip("set XEET_LIVE_ARTICLE_TWEET_ID to a post that carries an X article")
	}
	mgr, err := config.NewConfigManager()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	client := NewWebClient(cfg)

	page, err := client.FetchTweetDetail(ctx, tweetID, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) == 0 || page.Posts[0].ID != tweetID {
		t.Fatalf("focal post %s missing from its own detail page", tweetID)
	}
	post := page.Posts[0]
	if post.Article == nil {
		t.Fatalf("post %s parsed without an article body; the opt-in or the field path drifted", tweetID)
	}
	if post.Article.Text == "" {
		t.Fatal("article parsed but plain_text is empty")
	}
	t.Logf("article title=%q body=%d chars", post.Article.Title, len(post.Article.Text))
}
