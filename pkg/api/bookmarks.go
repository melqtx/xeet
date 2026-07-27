package api

import "context"

const bookmarksOperation = "Bookmarks"

// FetchBookmarks returns a page of the authenticated user's bookmarks.
func (c *WebClient) FetchBookmarks(ctx context.Context, cursor string, count int) (*TimelinePage, error) {
	return c.fetchTimelineOp(ctx, bookmarksOperation, "", "XEET_BOOKMARKS_QID", count,
		func(count int) map[string]any {
			// TODO(human): verify this minimal variable set against a fresh HAR
			// because the sandbox cannot exercise the live endpoint.
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
