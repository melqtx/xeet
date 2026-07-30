package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	userByScreenNameOperation = "UserByScreenName"
	userTweetsOperation       = "UserTweets"
)

var userByScreenNameFeatures = map[string]bool{
	"responsive_web_graphql_exclude_directive_enabled":                  true,
	"responsive_web_graphql_skip_user_profile_image_extensions_enabled": false,
	"responsive_web_graphql_timeline_navigation_enabled":                true,
	"responsive_web_twitter_article_notes_tab_enabled":                  true,
	"verified_phone_label_enabled":                                      false,
}

// FetchUserByScreenName resolves a handle to the numeric user id UserTweets
// expects. The id is not derivable from timeline posts (they carry only the
// handle), so profile browsing pays one extra request per user switch and
// caches the result in the column.
func (c *WebClient) FetchUserByScreenName(ctx context.Context, handle string) (string, error) {
	handle = strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if handle == "" {
		return "", errors.New("profile handle is empty")
	}
	if c.authToken == "" || c.ct0 == "" {
		return "", fmt.Errorf("no session; run 'xeet auth' first")
	}
	qid := c.operationQueryID(userByScreenNameOperation, "", "XEET_USERBYSCREENNAME_QID")
	if qid == "" {
		fresh, discoverErr := c.discoverOperation(ctx, userByScreenNameOperation)
		if discoverErr != nil {
			return "", fmt.Errorf("discover %s endpoint: %w", userByScreenNameOperation, discoverErr)
		}
		qid = fresh
	}
	res, err := c.doUserByScreenName(ctx, qid, handle)
	if err != nil {
		return "", err
	}
	if needsQueryIDRefresh(res) {
		fresh, discoverErr := c.discoverOperation(ctx, userByScreenNameOperation)
		if discoverErr != nil {
			return "", fmt.Errorf("user lookup endpoint changed and discovery failed: %w", discoverErr)
		}
		res, err = c.doUserByScreenName(ctx, fresh, handle)
		if err != nil {
			return "", err
		}
	}
	if err := statusToError(res.status, res.header); err != nil {
		return "", err
	}
	if isTransientStatus(res.status) {
		return "", &ServiceUnavailableError{Status: res.status}
	}
	if res.status != http.StatusOK {
		return "", fmt.Errorf("user lookup API error %d: %s", res.status, truncate(res.body))
	}

	var payload any
	if err := json.Unmarshal(res.body, &payload); err != nil {
		return "", fmt.Errorf("decode user lookup: %w", err)
	}
	if err := graphQLError(payload); err != nil {
		return "", err
	}
	if id := userIDFromLookup(payload); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("x returned no user for @%s", handle)
}

func (c *WebClient) doUserByScreenName(ctx context.Context, qid, handle string) (*httpResult, error) {
	variables, _ := json.Marshal(map[string]any{
		"screen_name":              handle,
		"withSafetyModeUserFields": true,
	})
	features, _ := json.Marshal(userByScreenNameFeatures)
	params := url.Values{}
	params.Set("variables", string(variables))
	params.Set("features", string(features))
	endpoint := fmt.Sprintf("https://x.com/i/api/graphql/%s/UserByScreenName?%s", qid, params.Encode())
	return c.send(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		return req, nil
	}, true, 4<<20)
}

func userIDFromLookup(payload any) string {
	root, _ := payload.(map[string]any)
	data, _ := root["data"].(map[string]any)
	user, _ := data["user"].(map[string]any)
	result, _ := user["result"].(map[string]any)
	id, _ := result["rest_id"].(string)
	return id
}

// FetchUserTweets returns one page of a user's posts (and their reposts —
// the endpoint mixes them, so callers must not assume every post belongs to
// the profile owner).
func (c *WebClient) FetchUserTweets(ctx context.Context, userID, cursor string, count int) (*TimelinePage, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, errors.New("profile user id is empty")
	}
	return c.fetchTimelineOp(ctx, userTweetsOperation, "", "XEET_USERTWEETS_QID", count, false,
		func(count int) map[string]any {
			variables := map[string]any{
				"userId":                 userID,
				"count":                  count,
				"includePromotedContent": false,
				"withVoice":              true,
				"withV2Timeline":         true,
			}
			if cursor != "" {
				variables["cursor"] = cursor
			}
			return variables
		})
}
