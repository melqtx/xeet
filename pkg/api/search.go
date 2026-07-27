package api

import (
	"context"
	"errors"
	"strings"
)

const searchOperation = "SearchTimeline"

// FetchSearchTimeline returns a page of search results for query.
func (c *WebClient) FetchSearchTimeline(ctx context.Context, query, cursor string, count int) (*TimelinePage, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("search query is empty")
	}
	// Unlike the home, following, and bookmark timelines, SearchTimeline answers
	// 404 with an empty body when the transaction header is missing, so the
	// header is not optional here despite this being a read.
	return c.fetchTimelineOp(ctx, searchOperation, "", "XEET_SEARCHTIMELINE_QID", count, true,
		func(count int) map[string]any {
			variables := map[string]any{
				"rawQuery":    query,
				"count":       count,
				"querySource": "typed_query",
				"product":     "Top",
			}
			if cursor != "" {
				variables["cursor"] = cursor
			}
			return variables
		})
}
