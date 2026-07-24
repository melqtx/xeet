package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const defaultViewerQueryID = "u4ni7JqpqdAQxWQfkLsdUQ"

// Account is the identity attached to the imported browser session.
type Account struct {
	ID       string
	Name     string
	Handle   string
	Verified bool
}

var viewerFeatures = map[string]bool{
	"responsive_web_graphql_exclude_directive_enabled":                  true,
	"responsive_web_graphql_skip_user_profile_image_extensions_enabled": false,
	"responsive_web_graphql_timeline_navigation_enabled":                true,
	"responsive_web_twitter_article_notes_tab_enabled":                  true,
	"verified_phone_label_enabled":                                      false,
}

// FetchViewer returns the account represented by the client's session cookies.
// It is a read-only request and refreshes X's rotating Viewer operation id once
// when necessary.
func (c *WebClient) FetchViewer(ctx context.Context) (*Account, error) {
	if c.authToken == "" || c.ct0 == "" {
		return nil, fmt.Errorf("no session; run 'xeet auth' first")
	}
	qid := c.operationQueryID("Viewer", defaultViewerQueryID, "XEET_VIEWER_QID")
	res, err := c.doViewer(ctx, qid)
	if err != nil {
		return nil, err
	}
	if needsQueryIDRefresh(res) {
		fresh, discoverErr := c.discoverOperation(ctx, "Viewer")
		if discoverErr != nil {
			return nil, fmt.Errorf("account identity endpoint changed and discovery failed: %w", discoverErr)
		}
		res, err = c.doViewer(ctx, fresh)
		if err != nil {
			return nil, err
		}
	}
	if err := statusToError(res.status, res.header); err != nil {
		return nil, err
	}
	if isTransientStatus(res.status) {
		return nil, &ServiceUnavailableError{Status: res.status}
	}
	if res.status != http.StatusOK {
		return nil, fmt.Errorf("account identity API error %d: %s", res.status, truncate(res.body))
	}

	var payload any
	if err := json.Unmarshal(res.body, &payload); err != nil {
		return nil, fmt.Errorf("decode account identity: %w", err)
	}
	if err := graphQLError(payload); err != nil {
		return nil, err
	}
	account, ok := accountFromViewer(payload)
	if !ok || account.Handle == "" {
		return nil, fmt.Errorf("x returned no account identity for this session")
	}
	return &account, nil
}

func (c *WebClient) doViewer(ctx context.Context, qid string) (*httpResult, error) {
	variables, _ := json.Marshal(map[string]bool{"withCommunitiesMemberships": true})
	features, _ := json.Marshal(viewerFeatures)
	params := url.Values{}
	params.Set("variables", string(variables))
	params.Set("features", string(features))
	endpoint := fmt.Sprintf("https://x.com/i/api/graphql/%s/Viewer?%s", qid, params.Encode())
	return c.send(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		return req, nil
	}, true, 4<<20)
}

func accountFromViewer(payload any) (Account, bool) {
	root, _ := payload.(map[string]any)
	data, _ := root["data"].(map[string]any)
	viewer := data["viewer"]
	return findViewerAccount(viewer)
}

func findViewerAccount(node any) (Account, bool) {
	switch value := node.(type) {
	case map[string]any:
		core, _ := value["core"].(map[string]any)
		legacy, _ := value["legacy"].(map[string]any)
		handle, _ := core["screen_name"].(string)
		name, _ := core["name"].(string)
		if handle == "" {
			handle, _ = legacy["screen_name"].(string)
		}
		if name == "" {
			name, _ = legacy["name"].(string)
		}
		if handle != "" {
			id, _ := value["rest_id"].(string)
			verified, _ := value["is_blue_verified"].(bool)
			if legacyVerified, _ := legacy["verified"].(bool); legacyVerified {
				verified = true
			}
			return Account{ID: id, Name: name, Handle: handle, Verified: verified}, true
		}
		for _, child := range value {
			if account, ok := findViewerAccount(child); ok {
				return account, true
			}
		}
	case []any:
		for _, child := range value {
			if account, ok := findViewerAccount(child); ok {
				return account, true
			}
		}
	}
	return Account{}, false
}
