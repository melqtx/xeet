package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	listLatestTweetsOperation    = "ListLatestTweetsTimeline"
	listsManagementPageOperation = "ListsManagementPageTimeline"
)

// ListInfo is one list the authenticated user owns or follows.
type ListInfo struct {
	ID          string
	Name        string
	MemberCount int
	IsPrivate   bool
}

// FetchListTimeline retrieves one page of the given X List's feed.
func (c *WebClient) FetchListTimeline(ctx context.Context, listID, cursor string, count int) (*TimelinePage, error) {
	return c.fetchTimelineOp(ctx, listLatestTweetsOperation, "", "XEET_LISTLATESTTWEETSTIMELINE_QID", count, false,
		func(count int) map[string]any {
			variables := map[string]any{
				"listId": listID,
				"count":  count,
			}
			if cursor != "" {
				variables["cursor"] = cursor
			}
			return variables
		})
}

// FetchOwnedLists enumerates the user's lists for the picker.
func (c *WebClient) FetchOwnedLists(ctx context.Context) ([]ListInfo, error) {
	return c.fetchListsOp(ctx, listsManagementPageOperation, "", "XEET_LISTSMANAGEMENTPAGETIMELINE_QID", 100, false,
		func(count int) map[string]any {
			return map[string]any{"count": count}
		})
}

func (c *WebClient) fetchListsOp(
	ctx context.Context,
	operation, fallback, environment string,
	count int,
	withTransactionID bool,
	buildVars func(count int) map[string]any,
) ([]ListInfo, error) {
	if c.authToken == "" || c.ct0 == "" {
		return nil, fmt.Errorf("no session; run 'xeet auth' first")
	}
	if count <= 0 || count > 100 {
		count = 30
	}
	qid := c.operationQueryID(operation, fallback, environment)
	if qid == "" {
		fresh, discoverErr := c.discoverOperation(ctx, operation)
		if discoverErr != nil {
			return nil, fmt.Errorf("discover %s endpoint: %w", operation, discoverErr)
		}
		qid = fresh
	}

	res, err := c.doTimelineOp(ctx, operation, qid, buildVars(count), withTransactionID)
	if err != nil {
		return nil, err
	}
	if needsQueryIDRefresh(res) {
		fresh, discoverErr := c.discoverOperation(ctx, operation)
		if discoverErr != nil {
			return nil, fmt.Errorf("home timeline endpoint changed and discovery failed: %w", discoverErr)
		}
		res, err = c.doTimelineOp(ctx, operation, fresh, buildVars(count), withTransactionID)
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
		return nil, fmt.Errorf("timeline API error %d: %s", res.status, truncate(res.body))
	}

	var payload any
	if err := json.Unmarshal(res.body, &payload); err != nil {
		return nil, fmt.Errorf("decode timeline: %w", err)
	}
	root, ok := payload.(map[string]any)
	if !ok || root["data"] == nil {
		return nil, fmt.Errorf("x returned a malformed lists response")
	}
	// X answers this operation 200 with the list entries intact *and* a partial
	// serialization error beside them, so the error is only fatal when nothing
	// parsed — which is also the shape an auth or rate-limit rejection takes,
	// leaving those classified as before.
	lists := parseListsPage(payload)
	if len(lists) > 0 {
		return lists, nil
	}
	if err := graphQLError(payload); err != nil {
		return nil, err
	}
	return lists, nil
}

func parseListsPage(payload any) []ListInfo {
	var lists []ListInfo
	seen := map[string]bool{}

	parseItemContent := func(raw any) {
		item, ok := raw.(map[string]any)
		if !ok {
			return
		}
		list, ok := parseListItem(item)
		if !ok || seen[list.ID] {
			return
		}
		seen[list.ID] = true
		lists = append(lists, list)
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
		if item, ok := content["itemContent"].(map[string]any); ok {
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
					parseItemContent(itemContent)
				}
			}
		}
	}

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
	return lists
}

func parseListItem(item map[string]any) (ListInfo, bool) {
	for _, key := range []string{"twitter_list", "list", "list_results"} {
		if list, ok := parseListNode(item[key], 0); ok {
			return list, true
		}
	}
	itemType := firstString(item, "itemType", "__typename", "entryType")
	if strings.Contains(strings.ToLower(itemType), "list") {
		return parseListNode(item, 0)
	}
	return ListInfo{}, false
}

func parseListNode(node any, depth int) (ListInfo, bool) {
	if depth > 4 {
		return ListInfo{}, false
	}
	object, ok := node.(map[string]any)
	if !ok {
		return ListInfo{}, false
	}
	if list, ok := listInfoFromObject(object); ok {
		return list, true
	}
	for _, key := range []string{"result", "twitter_list", "list", "list_results"} {
		if list, ok := parseListNode(object[key], depth+1); ok {
			return list, true
		}
	}
	return ListInfo{}, false
}

func listInfoFromObject(object map[string]any) (ListInfo, bool) {
	legacy, _ := object["legacy"].(map[string]any)
	id := firstString(object, "id_str", "rest_id", "id")
	if id == "" {
		id = firstString(legacy, "id_str", "rest_id", "id")
	}
	name := firstString(object, "name")
	if name == "" {
		name = firstString(legacy, "name")
	}
	if id == "" || name == "" {
		return ListInfo{}, false
	}

	memberCount := intValue(object["member_count"])
	if memberCount == 0 {
		memberCount = intValue(legacy["member_count"])
	}
	isPrivate := boolValue(object, "is_private", "private")
	if !isPrivate {
		isPrivate = boolValue(legacy, "is_private", "private")
	}
	mode := firstString(object, "mode", "privacy")
	if mode == "" {
		mode = firstString(legacy, "mode", "privacy")
	}
	return ListInfo{
		ID:          id,
		Name:        name,
		MemberCount: memberCount,
		IsPrivate:   isPrivate || strings.EqualFold(mode, "private"),
	}, true
}

func boolValue(object map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := object[key].(bool); ok {
			return value
		}
	}
	return false
}
