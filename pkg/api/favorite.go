package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	defaultFavoriteTweetQueryID   = "lI07N6Otwv1PhnEgXILM7A"
	defaultUnfavoriteTweetQueryID = "ZYKSe-w7KEslx3JhSIk5LA"
)

// SetTweetLiked likes or unlikes a post using X's web GraphQL mutation.
func (c *WebClient) SetTweetLiked(ctx context.Context, tweetID string, liked bool) error {
	if c.authToken == "" || c.ct0 == "" {
		return fmt.Errorf("no session — run 'xeet auth' first")
	}
	if tweetID == "" {
		return fmt.Errorf("tweet id is empty")
	}
	operation, qid := "FavoriteTweet", defaultFavoriteTweetQueryID
	if !liked {
		operation, qid = "UnfavoriteTweet", defaultUnfavoriteTweetQueryID
	}
	status, body, err := c.doTweetLike(ctx, operation, qid, tweetID)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		fresh, discoverErr := DiscoverOperationQueryID(ctx, c.authToken, c.ct0, operation)
		if discoverErr != nil {
			return fmt.Errorf("%s endpoint changed and discovery failed: %w", operation, discoverErr)
		}
		status, body, err = c.doTweetLike(ctx, operation, fresh, tweetID)
		if err != nil {
			return err
		}
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("session expired — run 'xeet auth' again")
	}
	if status != http.StatusOK {
		return fmt.Errorf("like API error %d: %s", status, truncate(body))
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode like response: %w", err)
	}
	if message := firstGraphQLError(payload); message != "" {
		return fmt.Errorf("x graphql error: %s", message)
	}
	return nil
}

func (c *WebClient) doTweetLike(ctx context.Context, operation, qid, tweetID string) (int, []byte, error) {
	payload := map[string]any{
		"variables": map[string]string{"tweet_id": tweetID},
		"queryId":   qid,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	endpoint := fmt.Sprintf("https://x.com/i/api/graphql/%s/%s", qid, operation)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("update like: %w", err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return resp.StatusCode, responseBody, nil
}
