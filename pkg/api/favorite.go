package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	defaultFavoriteTweetQueryID   = "lI07N6Otwv1PhnEgXILM7A"
	defaultUnfavoriteTweetQueryID = "ZYKSe-w7KEslx3JhSIk5LA"
)

// SetTweetLiked likes or unlikes a post using X's web GraphQL mutation.
func (c *WebClient) SetTweetLiked(ctx context.Context, tweetID string, liked bool) error {
	if c.authToken == "" || c.ct0 == "" {
		return fmt.Errorf("no session; run 'xeet auth' first")
	}
	if tweetID == "" {
		return fmt.Errorf("tweet id is empty")
	}
	operation, qid := "FavoriteTweet", defaultFavoriteTweetQueryID
	if !liked {
		operation, qid = "UnfavoriteTweet", defaultUnfavoriteTweetQueryID
	}
	res, err := c.doTweetLike(ctx, operation, qid, tweetID)
	if err != nil {
		return err
	}
	if needsQueryIDRefresh(res) {
		fresh, discoverErr := c.discoverOperation(ctx, operation)
		if discoverErr != nil {
			return fmt.Errorf("%s endpoint changed and discovery failed: %w", operation, discoverErr)
		}
		res, err = c.doTweetLike(ctx, operation, fresh, tweetID)
		if err != nil {
			return err
		}
	}
	if err := statusToError(res.status, res.header); err != nil {
		return err
	}
	if res.status != http.StatusOK {
		return fmt.Errorf("like API error %d: %s", res.status, truncate(res.body))
	}
	var payload any
	if err := json.Unmarshal(res.body, &payload); err != nil {
		return fmt.Errorf("decode like response: %w", err)
	}
	if err := graphQLError(payload); err != nil {
		return err
	}
	return nil
}

// doTweetLike is idempotent (liking twice is a no-op), so transient failures
// are retried.
func (c *WebClient) doTweetLike(ctx context.Context, operation, qid, tweetID string) (*httpResult, error) {
	payload := map[string]any{
		"variables": map[string]string{"tweet_id": tweetID},
		"queryId":   qid,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("https://x.com/i/api/graphql/%s/%s", qid, operation)
	return c.send(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		return req, nil
	}, true, 2<<20)
}
