package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	createRetweetOperation = "CreateRetweet"
	deleteRetweetOperation = "DeleteRetweet"
	// The retweet mutations answer a bodyless 404 when the transaction header
	// is missing — the same shape search.go documents for SearchTimeline, and
	// what the first live run returned. Like CreateTweet, every attempt mints
	// a fresh one.
	retweetWithTransactionID = true
)

// SetTweetReposted reposts or un-reposts a post using X's web GraphQL
// mutations. Unlike likes there is no shipped fallback query id: the id is
// discovered from X's live bundles (or supplied via XEET_CREATERETWEET_QID /
// XEET_DELETERETWEET_QID) and cached in the config.
func (c *WebClient) SetTweetReposted(ctx context.Context, tweetID string, reposted bool) error {
	if c.authToken == "" || c.ct0 == "" {
		return fmt.Errorf("no session; run 'xeet auth' first")
	}
	if tweetID == "" {
		return fmt.Errorf("tweet id is empty")
	}
	operation, environment := createRetweetOperation, "XEET_CREATERETWEET_QID"
	if !reposted {
		operation, environment = deleteRetweetOperation, "XEET_DELETERETWEET_QID"
	}
	qid := c.operationQueryID(operation, "", environment)
	if qid == "" {
		fresh, discoverErr := c.discoverOperation(ctx, operation)
		if discoverErr != nil {
			return fmt.Errorf("discover %s endpoint: %w", operation, discoverErr)
		}
		qid = fresh
	}
	res, err := c.doTweetRetweet(ctx, operation, qid, tweetID)
	if err != nil {
		return err
	}
	if needsQueryIDRefresh(res) {
		fresh, discoverErr := c.discoverOperation(ctx, operation)
		if discoverErr != nil {
			return fmt.Errorf("%s endpoint changed and discovery failed: %w", operation, discoverErr)
		}
		res, err = c.doTweetRetweet(ctx, operation, fresh, tweetID)
		if err != nil {
			return err
		}
	}
	if err := statusToError(res.status, res.header); err != nil {
		return err
	}
	if isTransientStatus(res.status) {
		return &ServiceUnavailableError{Status: res.status}
	}
	if res.status != http.StatusOK {
		return fmt.Errorf("repost API error %d: %s", res.status, truncate(res.body))
	}
	var payload any
	if err := json.Unmarshal(res.body, &payload); err != nil {
		return fmt.Errorf("decode repost response: %w", err)
	}
	if err := graphQLError(payload); err != nil {
		return err
	}
	return nil
}

// doTweetRetweet never retries transient failures: CreateRetweet is not
// idempotent (a replayed request errors as "already retweeted"), so a timed-out
// request that actually landed must not be re-fired. The TUI rolls its
// optimistic state back and lets the user press the key again.
func (c *WebClient) doTweetRetweet(ctx context.Context, operation, qid, tweetID string) (*httpResult, error) {
	variable := "tweet_id"
	if operation == deleteRetweetOperation {
		variable = retweetDeleteVariable()
	}
	payload := map[string]any{
		"variables": map[string]string{variable: tweetID},
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
		// Minted per attempt (see doTimelineOp): a replayed id is rejected
		// exactly like an omitted one.
		if retweetWithTransactionID && c.transactionID != nil {
			transactionID, err := c.transactionID(ctx, req.Method, req.URL.Path)
			if err != nil {
				return nil, fmt.Errorf("generate X transaction id: %w", err)
			}
			req.Header.Set("X-Client-Transaction-Id", transactionID)
		}
		return req, nil
	}, false, 2<<20)
}

// retweetDeleteVariable names the DeleteRetweet input. Confirmed against the
// live endpoint: it takes the original tweet's id, and a wrong name earns a
// GraphQL validation error naming this one.
func retweetDeleteVariable() string {
	return "source_tweet_id"
}
