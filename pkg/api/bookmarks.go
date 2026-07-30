package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	bookmarksOperation      = "Bookmarks"
	createBookmarkOperation = "CreateBookmark"
	deleteBookmarkOperation = "DeleteBookmark"
	// The first live run answered a bodyless 404 without the transaction
	// header — the same shape search.go and retweet.go document — so every
	// attempt mints a fresh one, exactly like the retweet mutations.
	bookmarkWithTransactionID = true
)

// FetchBookmarks returns a page of the authenticated user's bookmarks.
func (c *WebClient) FetchBookmarks(ctx context.Context, cursor string, count int) (*TimelinePage, error) {
	return c.fetchTimelineOp(ctx, bookmarksOperation, "", "XEET_BOOKMARKS_QID", count, false,
		func(count int) map[string]any {
			variables := map[string]any{
				"count":                  count,
				"includePromotedContent": false,
			}
			if cursor != "" {
				variables["cursor"] = cursor
			}
			return variables
		})
}

// SetTweetBookmarked bookmarks or un-bookmarks a post using X's web GraphQL
// mutations. Like the retweet pair there is no shipped fallback query id: the
// id is discovered from X's live bundles (or supplied via
// XEET_CREATEBOOKMARK_QID / XEET_DELETEBOOKMARK_QID) and cached in the config.
func (c *WebClient) SetTweetBookmarked(ctx context.Context, tweetID string, bookmarked bool) error {
	if c.authToken == "" || c.ct0 == "" {
		return fmt.Errorf("no session; run 'xeet auth' first")
	}
	if tweetID == "" {
		return fmt.Errorf("tweet id is empty")
	}
	operation, environment := createBookmarkOperation, "XEET_CREATEBOOKMARK_QID"
	if !bookmarked {
		operation, environment = deleteBookmarkOperation, "XEET_DELETEBOOKMARK_QID"
	}
	qid := c.operationQueryID(operation, "", environment)
	if qid == "" {
		fresh, discoverErr := c.discoverOperation(ctx, operation)
		if discoverErr != nil {
			return fmt.Errorf("discover %s endpoint: %w", operation, discoverErr)
		}
		qid = fresh
	}
	res, err := c.doTweetBookmark(ctx, operation, qid, tweetID)
	if err != nil {
		return err
	}
	if needsQueryIDRefresh(res) {
		fresh, discoverErr := c.discoverOperation(ctx, operation)
		if discoverErr != nil {
			return fmt.Errorf("%s endpoint changed and discovery failed: %w", operation, discoverErr)
		}
		res, err = c.doTweetBookmark(ctx, operation, fresh, tweetID)
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
		return fmt.Errorf("bookmark API error %d: %s", res.status, truncate(res.body))
	}
	var payload any
	if err := json.Unmarshal(res.body, &payload); err != nil {
		return fmt.Errorf("decode bookmark response: %w", err)
	}
	if err := graphQLError(payload); err != nil {
		return err
	}
	return nil
}

// doTweetBookmark retries only CreateBookmark: re-adding a bookmark is a
// no-op, but the live endpoint answers a replayed delete with a GraphQL
// "_Missing: not found in actor's favorites" error. A timed-out delete that
// actually landed must not be re-fired, so DeleteBookmark gets a single
// attempt — the same call CreateRetweet makes.
func (c *WebClient) doTweetBookmark(ctx context.Context, operation, qid, tweetID string) (*httpResult, error) {
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
		// Minted per attempt (see doTimelineOp): a replayed id is rejected
		// exactly like an omitted one.
		if bookmarkWithTransactionID && c.transactionID != nil {
			transactionID, err := c.transactionID(ctx, req.Method, req.URL.Path)
			if err != nil {
				return nil, fmt.Errorf("generate X transaction id: %w", err)
			}
			req.Header.Set("X-Client-Transaction-Id", transactionID)
		}
		return req, nil
	}, operation == createBookmarkOperation, 2<<20)
}
