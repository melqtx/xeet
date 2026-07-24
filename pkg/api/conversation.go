package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ConversationPost is one post in a TweetDetail conversation. Depth is
// relative to the focal post (the focal post itself has depth zero).
type ConversationPost struct {
	TimelinePost
	Depth int
}

// ConversationPage is one ordered page from X's TweetDetail timeline.
type ConversationPage struct {
	Posts      []ConversationPost
	Unresolved []TimelinePost
	Cursor     string
}

// FetchTweetDetail retrieves a post and the replies X returns for it.
func (c *WebClient) FetchTweetDetail(ctx context.Context, tweetID, cursor string, count int) (*ConversationPage, error) {
	if c.authToken == "" || c.ct0 == "" {
		return nil, fmt.Errorf("no session; run 'xeet auth' first")
	}
	if tweetID == "" {
		return nil, fmt.Errorf("tweet id is required")
	}
	if count <= 0 || count > 100 {
		count = 40
	}

	qid := c.operationQueryID("TweetDetail", "", "XEET_TWEETDETAIL_QID")
	if qid == "" {
		fresh, err := c.discoverOperation(ctx, "TweetDetail")
		if err != nil {
			return nil, fmt.Errorf("discover tweet detail endpoint: %w", err)
		}
		qid = fresh
	}
	res, err := c.doTweetDetail(ctx, qid, tweetID, cursor, count)
	if err != nil {
		return nil, err
	}
	if needsQueryIDRefresh(res) {
		fresh, discoverErr := c.discoverOperation(ctx, "TweetDetail")
		if discoverErr != nil {
			return nil, fmt.Errorf("tweet detail endpoint changed and discovery failed: %w", discoverErr)
		}
		res, err = c.doTweetDetail(ctx, fresh, tweetID, cursor, count)
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
		return nil, fmt.Errorf("tweet detail API error %d: %s", res.status, truncate(res.body))
	}

	var payload any
	if err := json.Unmarshal(res.body, &payload); err != nil {
		return nil, fmt.Errorf("decode tweet detail: %w", err)
	}
	root, ok := payload.(map[string]any)
	if !ok || root["data"] == nil {
		return nil, fmt.Errorf("x returned a malformed tweet detail response")
	}
	if err := graphQLError(payload); err != nil {
		return nil, err
	}
	page := parseConversation(payload, tweetID)
	return &page, nil
}

// doTweetDetail is a read, so transient failures are retried.
func (c *WebClient) doTweetDetail(ctx context.Context, qid, tweetID, cursor string, count int) (*httpResult, error) {
	variables := map[string]any{
		"focalTweetId":                           tweetID,
		"referrer":                               "tweet",
		"with_rux_injections":                    false,
		"rankingMode":                            "Relevance",
		"includePromotedContent":                 false,
		"withCommunity":                          true,
		"withQuickPromoteEligibilityTweetFields": true,
		"withBirdwatchNotes":                     true,
		"withVoice":                              true,
	}
	if cursor != "" {
		variables["cursor"] = cursor
	}
	// TweetDetail has changed its count variable over time. Sending it is
	// harmless when ignored and keeps continuation pages bounded when honored.
	variables["count"] = count
	variablesJSON, _ := json.Marshal(variables)
	featuresJSON, _ := json.Marshal(timelineFeatures)
	fieldTogglesJSON, _ := json.Marshal(timelineFieldToggles)

	params := url.Values{}
	params.Set("variables", string(variablesJSON))
	params.Set("features", string(featuresJSON))
	params.Set("fieldToggles", string(fieldTogglesJSON))
	endpoint := fmt.Sprintf("https://x.com/i/api/graphql/%s/TweetDetail?%s", qid, params.Encode())
	return c.send(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		return req, nil
	}, true, 20<<20)
}

func parseConversation(payload any, focalID string) ConversationPage {
	page := ConversationPage{}
	seen := map[string]bool{}
	var ordered []TimelinePost

	parseItemContent := func(raw any) {
		item, ok := raw.(map[string]any)
		if !ok {
			return
		}
		if post, ok := parseTimelineItem(item); ok && !seen[post.ID] {
			seen[post.ID] = true
			ordered = append(ordered, post)
		}
	}

	parseEntry := func(raw any) {
		entry, ok := raw.(map[string]any)
		if !ok {
			return
		}
		content, _ := entry["content"].(map[string]any)
		if content == nil {
			content = entry
		}
		parseCursor := func(item map[string]any) {
			cursorType, _ := item["cursorType"].(string)
			kind := strings.ToLower(cursorType)
			if kind == "bottom" || strings.Contains(kind, "showmore") {
				if value, _ := item["value"].(string); value != "" {
					page.Cursor = value
				}
			}
		}
		parseCursor(content)
		if item, ok := content["itemContent"].(map[string]any); ok {
			parseCursor(item)
			parseItemContent(item)
		}
		if items, ok := content["items"].([]any); ok {
			for _, rawItem := range items {
				moduleItem, _ := rawItem.(map[string]any)
				item, _ := moduleItem["item"].(map[string]any)
				if item == nil {
					item = moduleItem
				}
				if itemContent, ok := item["itemContent"].(map[string]any); ok {
					parseCursor(itemContent)
					parseItemContent(itemContent)
				}
			}
		}
	}

	// Locate instruction entry arrays, then parse each array in API order.
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if entries, ok := value["entries"].([]any); ok {
				for _, entry := range entries {
					parseEntry(entry)
				}
				return
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(payload)

	byID := make(map[string]TimelinePost, len(ordered))
	for _, post := range ordered {
		byID[post.ID] = post
	}
	depthOf := func(post TimelinePost) (int, bool) {
		if post.ID == focalID {
			return 0, true
		}
		depth := 1
		parent := post.InReplyToID
		visited := map[string]bool{post.ID: true}
		for parent != "" && !visited[parent] {
			if parent == focalID {
				return depth, true
			}
			visited[parent] = true
			ancestor, ok := byID[parent]
			if !ok {
				return 0, false
			}
			parent = ancestor.InReplyToID
			depth++
		}
		return 0, false
	}

	// The API may place ancestors before the focal post. Keep only the focal
	// post and its descendants, while preserving reply order from X.
	if focal, ok := byID[focalID]; ok {
		page.Posts = append(page.Posts, ConversationPost{TimelinePost: focal})
	}
	for _, post := range ordered {
		if post.ID == focalID {
			continue
		}
		if depth, ok := depthOf(post); ok {
			page.Posts = append(page.Posts, ConversationPost{TimelinePost: post, Depth: depth})
		} else if post.InReplyToID != "" {
			// A continuation can omit a nested reply's parent because that parent
			// was delivered on an earlier page. The model resolves these against
			// its accumulated conversation; ancestors and unrelated posts remain
			// unresolved and are not displayed.
			page.Unresolved = append(page.Unresolved, post)
		}
	}
	return page
}
