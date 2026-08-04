package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	notificationsOperation        = "NotificationsTimeline"
	defaultNotificationsQueryID   = "2FvqvnMOYuY5EEh--vxdFQ"
	notificationsQueryEnvironment = "XEET_NOTIFICATIONSTIMELINE_QID"
)

// NotificationKind identifies the actionable conversation events Xeet shows.
type NotificationKind string

const (
	NotificationReply   NotificationKind = "reply"
	NotificationMention NotificationKind = "mention"
)

// Notification is a reply or mention with the post that caused it.
type Notification struct {
	ID   string
	Kind NotificationKind
	Post TimelinePost
}

// NotificationPage is one page from the authenticated notification timeline.
type NotificationPage struct {
	Notifications []Notification
	Cursor        string
	AccountID     string
}

// FetchNotifications returns actionable replies and mentions from X's
// NotificationsTimeline. Other notification unions (likes, follows, reposts)
// are intentionally ignored because they have no post Xeet can reply to.
func (c *WebClient) FetchNotifications(ctx context.Context, cursor string, count int) (*NotificationPage, error) {
	if c.authToken == "" || c.ct0 == "" {
		return nil, fmt.Errorf("no session; run 'xeet auth' first")
	}
	if count <= 0 || count > 100 {
		count = 20
	}
	qid := c.operationQueryID(notificationsOperation, defaultNotificationsQueryID, notificationsQueryEnvironment)
	if qid == "" {
		fresh, err := c.discoverOperation(ctx, notificationsOperation)
		if err != nil {
			return nil, fmt.Errorf("discover notifications endpoint: %w", err)
		}
		qid = fresh
	}
	variables := map[string]any{"timeline_type": "All", "count": count}
	if cursor != "" {
		variables["cursor"] = cursor
	}

	res, err := c.doTimelineOp(ctx, notificationsOperation, qid, variables, true)
	if err != nil {
		return nil, err
	}
	if needsQueryIDRefresh(res) {
		fresh, discoverErr := c.discoverOperation(ctx, notificationsOperation)
		if discoverErr != nil {
			return nil, fmt.Errorf("notifications endpoint changed and discovery failed: %w", discoverErr)
		}
		res, err = c.doTimelineOp(ctx, notificationsOperation, fresh, variables, true)
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
		return nil, fmt.Errorf("notifications API error %d: %s", res.status, truncate(res.body))
	}

	var payload any
	if err := json.Unmarshal(res.body, &payload); err != nil {
		return nil, fmt.Errorf("decode notifications: %w", err)
	}
	if err := graphQLError(payload); err != nil {
		return nil, err
	}
	root, ok := payload.(map[string]any)
	if !ok || root["data"] == nil {
		return nil, fmt.Errorf("x returned a malformed notifications response")
	}
	page := parseNotifications(payload)
	return &page, nil
}

func parseNotifications(payload any) NotificationPage {
	page := NotificationPage{AccountID: notificationAccountID(payload)}
	seen := map[string]bool{}

	parseItem := func(raw any) {
		item, ok := raw.(map[string]any)
		if !ok {
			return
		}
		itemType, _ := item["itemType"].(string)
		typename, _ := item["__typename"].(string)
		// TimelineNotification is used for likes, follows, and grouped actions.
		// Its nested target tweet is not the post that caused a reply/mention.
		if itemType == "TimelineNotification" || typename == "TimelineNotification" {
			return
		}
		if itemType != "" && itemType != "TimelineTweet" {
			return
		}
		post, ok := parseTimelineItem(item)
		if !ok || seen[post.ID] {
			return
		}
		seen[post.ID] = true
		kind := NotificationMention
		if post.InReplyToID != "" {
			kind = NotificationReply
		}
		page.Notifications = append(page.Notifications, Notification{ID: post.ID, Kind: kind, Post: post})
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

	parseEntry := func(raw any) {
		entry, ok := raw.(map[string]any)
		if !ok {
			return
		}
		content, _ := entry["content"].(map[string]any)
		if content == nil {
			content = entry
		}
		parseCursor(content)
		if item, ok := content["itemContent"].(map[string]any); ok {
			parseCursor(item)
			parseItem(item)
		}
		if items, ok := content["items"].([]any); ok {
			for _, rawItem := range items {
				module, _ := rawItem.(map[string]any)
				item, _ := module["item"].(map[string]any)
				if item == nil {
					item = module
				}
				if itemContent, ok := item["itemContent"].(map[string]any); ok {
					parseCursor(itemContent)
					parseItem(itemContent)
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
	return page
}

func notificationAccountID(payload any) string {
	root, _ := payload.(map[string]any)
	data, _ := root["data"].(map[string]any)
	viewer, _ := data["viewer_v2"].(map[string]any)
	results, _ := viewer["user_results"].(map[string]any)
	result, _ := results["result"].(map[string]any)
	id, _ := result["rest_id"].(string)
	return id
}
