package api

import "context"

const bookmarksOperation = "Bookmarks"

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
