package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// SetTweetReposted changes the viewer's repost state. A transport failure is
// not retried: the mutation may already have landed.
func (c *WebClient) SetTweetReposted(ctx context.Context, tweetID string, reposted bool) error {
	if c.authToken == "" || c.ct0 == "" {
		return fmt.Errorf("no session; run 'xeet auth' first")
	}
	if tweetID == "" {
		return fmt.Errorf("tweet id is empty")
	}
	operation := "CreateRetweet"
	variables := map[string]any{"tweet_id": tweetID, "dark_request": false}
	if !reposted {
		operation = "DeleteRetweet"
		variables = map[string]any{"source_tweet_id": tweetID, "dark_request": false}
	}
	qid := c.operationQIDs[operation]
	if qid == "" {
		var err error
		qid, err = c.discoverOperation(ctx, operation)
		if err != nil {
			return fmt.Errorf("discover repost endpoint: %w", err)
		}
	}
	send := func(id string) (*httpResult, error) {
		body, err := json.Marshal(map[string]any{"variables": variables, "queryId": id})
		if err != nil {
			return nil, err
		}
		endpoint := fmt.Sprintf("https://x.com/i/api/graphql/%s/%s", id, operation)
		return c.send(ctx, func() (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			if err = c.setCreateTweetHeaders(req); err != nil {
				return nil, err
			}
			return req, nil
		}, false, 2<<20)
	}
	res, err := send(qid)
	if err != nil {
		return fmt.Errorf("repost not confirmed; check X before trying again: %w", err)
	}
	if needsQueryIDRefresh(res) {
		fresh, err := c.discoverOperation(ctx, operation)
		if err != nil {
			return err
		}
		res, err = send(fresh)
		if err != nil {
			return fmt.Errorf("repost not confirmed; check X before trying again: %w", err)
		}
	}
	if err := statusToError(res.status, res.header); err != nil {
		return err
	}
	if res.status != http.StatusOK {
		return fmt.Errorf("repost not confirmed (HTTP %d); check X before trying again", res.status)
	}
	var payload map[string]any
	if json.Unmarshal(res.body, &payload) != nil {
		return fmt.Errorf("repost not confirmed; check X before trying again")
	}
	if issues := collectGraphQLIssues(payload); len(issues) > 0 {
		return mapGraphQLError(issues[0].code, issues[0].message)
	}
	data, _ := payload["data"].(map[string]any)
	mutationKey, resultKey := "create_retweet", "retweet_results"
	if !reposted {
		mutationKey, resultKey = "unretweet", "source_tweet_results"
	}
	mutation, _ := data[mutationKey].(map[string]any)
	if !reposted && mutation == nil {
		mutation, _ = data["delete_retweet"].(map[string]any)
	}
	results, _ := mutation[resultKey].(map[string]any)
	id, _ := trustedPostID(results["result"], "", 0)
	if id == "" {
		return fmt.Errorf("repost not confirmed; check X before trying again")
	}
	return nil
}
