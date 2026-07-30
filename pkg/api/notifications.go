package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const notificationsTimelineOperation = "NotificationsTimeline"

// FetchNotificationsTimeline retrieves one page of the authenticated user's
// notifications. Each notification is shaped into a TimelinePost for the feed
// it points at so the usual actions (enter, like, reply) work unchanged.
func (c *WebClient) FetchNotificationsTimeline(ctx context.Context, cursor string, count int) (*TimelinePage, error) {
	return c.fetchNotificationsOp(ctx, notificationsTimelineOperation, "", "XEET_NOTIFICATIONSTIMELINE_QID", count, false,
		func(count int) map[string]any {
			variables := map[string]any{
				"count": count,
				// GRAPHQL_VALIDATION_FAILED names this when it is missing; the
				// web client's notifications page sends "All".
				"timeline_type": "All",
			}
			if cursor != "" {
				variables["cursor"] = cursor
			}
			return variables
		})
}

// fetchTimelineOp hard-calls parseTimeline, which silently drops items without
// a top-level tweet_results — exactly what notification entries look like. So
// this runs the same ladder (resolve, discover, refresh, classify, decode) and
// diverges only at the final parse, mirroring fetchListsOp.
func (c *WebClient) fetchNotificationsOp(
	ctx context.Context,
	operation, fallback, environment string,
	count int,
	withTransactionID bool,
	buildVars func(count int) map[string]any,
) (*TimelinePage, error) {
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
			return nil, fmt.Errorf("notifications endpoint changed and discovery failed: %w", discoverErr)
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
		return nil, fmt.Errorf("notifications API error %d: %s", res.status, truncate(res.body))
	}

	var payload any
	if err := json.Unmarshal(res.body, &payload); err != nil {
		return nil, fmt.Errorf("decode notifications: %w", err)
	}
	root, ok := payload.(map[string]any)
	if !ok || root["data"] == nil {
		return nil, fmt.Errorf("x returned a malformed notifications response")
	}
	if err := graphQLError(payload); err != nil {
		return nil, err
	}
	page := parseNotificationsPage(payload)
	return &page, nil
}

// The walker deliberately duplicates parseEntries' shape instead of sharing
// it, for the same reason parseEntries duplicates parseConversation's:
// timeline.go tracks upstream closely, and coupling the two would make
// rebases costlier without changing notification behavior.
func parseNotificationsPage(payload any) TimelinePage {
	var posts []TimelinePost
	var bottomCursor string
	seen := map[string]bool{}

	parseItemContent := func(raw any) {
		item, ok := raw.(map[string]any)
		if !ok {
			return
		}
		if post, ok := parseNotificationItem(item); ok && !seen[post.ID] {
			seen[post.ID] = true
			posts = append(posts, post)
		}
	}

	parseCursor := func(item map[string]any) {
		cursorType, _ := item["cursorType"].(string)
		kind := strings.ToLower(cursorType)
		if kind == "bottom" || strings.Contains(kind, "showmore") {
			if value, _ := item["value"].(string); value != "" {
				bottomCursor = value
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
	return TimelinePage{Posts: posts, Cursor: bottomCursor}
}

// parseNotificationItem maps one notification onto the post it targets. The
// post's own id and author go into ID/Handle (not the notification's) because
// every downstream action — enter, like, reply, postURL — keys off those
// fields. Notifications with no resolvable target post (follows, digests) are
// skipped rather than shown as dead rows no action works on.
func parseNotificationItem(item map[string]any) (TimelinePost, bool) {
	notification, _ := item["notification"].(map[string]any)
	if notification == nil {
		notification = item
	}
	kind := firstString(item, "notificationType", "notification_type")
	if kind == "" {
		kind = firstString(notification, "notification_type", "__typename")
	}
	message := notificationMessage(notification)
	if message == "" {
		message = notificationMessage(item)
	}

	// An embedded tweet result carries the real post state (counts, liked,
	// media), which keeps the like button honest; the url fallback only ever
	// yields an id and a handle.
	var post TimelinePost
	if embedded, ok := notificationEmbeddedPost(item, notification); ok {
		post = embedded
	} else {
		id, handle := notificationTargetRef(notification)
		if id == "" {
			return TimelinePost{}, false
		}
		post = TimelinePost{ID: id, Handle: handle, AuthorName: handle}
	}

	// A plain TimelineTweet entry (how mentions arrive) has no message; its
	// typename must not become a text prefix. The kind only fills in when
	// neither a message nor a post text exists.
	text := message
	if text == "" && post.Text == "" {
		text = kind
	}
	if post.Text != "" && text != "" {
		post.Text = text + "\n\n" + post.Text
	} else if post.Text == "" {
		post.Text = text
	}
	if post.ID == "" || post.Text == "" {
		return TimelinePost{}, false
	}
	if timestamp, _ := notification["timestamp_ms"].(string); timestamp != "" {
		if millis, err := strconv.ParseInt(timestamp, 10, 64); err == nil {
			post.CreatedAt = time.UnixMilli(millis)
		}
	}
	return post, true
}

func notificationMessage(notification map[string]any) string {
	for _, key := range []string{"message", "richMessage", "rich_message"} {
		rich, _ := notification[key].(map[string]any)
		if text, _ := rich["text"].(string); text != "" {
			return html.UnescapeString(text)
		}
	}
	return ""
}

func notificationEmbeddedPost(item, notification map[string]any) (TimelinePost, bool) {
	for _, node := range []any{item["tweet_results"], notification["tweet_results"]} {
		tweetResults, _ := node.(map[string]any)
		if tweetResults == nil {
			continue
		}
		if post, ok := parseTimelineItem(map[string]any{"tweet_results": tweetResults}); ok {
			return post, true
		}
	}
	return TimelinePost{}, false
}

// notificationTargetRef digs the target post id (and its author's handle) out
// of the notification's deeplink, shaped like
// "https://x.com/<handle>/status/<id>".
func notificationTargetRef(notification map[string]any) (id, handle string) {
	for _, key := range []string{"notification_url", "url"} {
		link, _ := notification[key].(map[string]any)
		raw := firstString(link, "url", "expanded_url")
		if raw == "" {
			continue
		}
		parts := strings.Split(strings.Trim(raw, "/"), "/")
		for i, part := range parts {
			if part != "status" || i+1 >= len(parts) || i == 0 {
				continue
			}
			candidate := strings.SplitN(parts[i+1], "?", 2)[0]
			if _, err := strconv.ParseInt(candidate, 10, 64); err == nil {
				return candidate, parts[i-1]
			}
		}
	}
	return "", ""
}
