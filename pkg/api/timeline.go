package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultHomeTimelineQueryID = "lqfNCpeO0wydVAAXAbAU5w"

type TimelineMedia struct {
	URL     string
	Type    string
	AltText string
	Width   int
	Height  int
}

type TimelinePost struct {
	ID          string
	Text        string
	AuthorName  string
	Handle      string
	CreatedAt   time.Time
	ReplyCount  int
	RepostCount int
	LikeCount   int
	ViewCount   string
	MediaCount  int
	Media       []TimelineMedia
	Liked       bool
}

type TimelinePage struct {
	Posts  []TimelinePost
	Cursor string
}

var timelineFeatures = map[string]bool{
	"rweb_video_screen_enabled":                                               true,
	"rweb_cashtags_enabled":                                                   true,
	"profile_label_improvements_pcf_label_in_post_enabled":                    true,
	"responsive_web_profile_redirect_enabled":                                 false,
	"rweb_tipjar_consumption_enabled":                                         true,
	"verified_phone_label_enabled":                                            false,
	"creator_subscriptions_tweet_preview_api_enabled":                         true,
	"responsive_web_graphql_timeline_navigation_enabled":                      true,
	"responsive_web_graphql_skip_user_profile_image_extensions_enabled":       false,
	"premium_content_api_read_enabled":                                        false,
	"communities_web_enable_tweet_community_results_fetch":                    true,
	"c9s_tweet_anatomy_moderator_badge_enabled":                               true,
	"responsive_web_grok_analyze_button_fetch_trends_enabled":                 false,
	"responsive_web_grok_analyze_post_followups_enabled":                      true,
	"rweb_cashtags_composer_attachment_enabled":                               false,
	"responsive_web_jetfuel_frame":                                            false,
	"responsive_web_grok_share_attachment_enabled":                            true,
	"responsive_web_grok_annotations_enabled":                                 false,
	"articles_preview_enabled":                                                true,
	"responsive_web_edit_tweet_api_enabled":                                   true,
	"rweb_conversational_replies_downvote_enabled":                            false,
	"graphql_is_translatable_rweb_tweet_is_translatable_enabled":              true,
	"view_counts_everywhere_api_enabled":                                      true,
	"longform_notetweets_consumption_enabled":                                 true,
	"responsive_web_twitter_article_tweet_consumption_enabled":                true,
	"content_disclosure_indicator_enabled":                                    true,
	"content_disclosure_ai_generated_indicator_enabled":                       true,
	"responsive_web_grok_show_grok_translated_post":                           false,
	"responsive_web_grok_analysis_button_from_backend":                        true,
	"post_ctas_fetch_enabled":                                                 false,
	"freedom_of_speech_not_reach_fetch_enabled":                               true,
	"standardized_nudges_misinfo":                                             true,
	"tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled": true,
	"longform_notetweets_rich_text_read_enabled":                              true,
	"longform_notetweets_inline_media_enabled":                                true,
	"responsive_web_grok_image_annotation_enabled":                            true,
	"responsive_web_grok_imagine_annotation_enabled":                          true,
	"responsive_web_grok_community_note_auto_translation_is_enabled":          false,
	"responsive_web_enhance_cards_enabled":                                    false,
}

var timelineFieldToggles = map[string]bool{
	"withPayments":                false,
	"withAuxiliaryUserLabels":     false,
	"withArticleRichContentState": false,
	"withArticlePlainText":        false,
	"withArticleSummaryText":      false,
	"withArticleVoiceOver":        false,
	"withGrokAnalyze":             false,
	"withDisallowedReplyControls": false,
}

// FetchHomeTimeline retrieves one page of the authenticated For You feed.
func (c *WebClient) FetchHomeTimeline(ctx context.Context, cursor string, count int) (*TimelinePage, error) {
	if c.authToken == "" || c.ct0 == "" {
		return nil, fmt.Errorf("no session — run 'xeet auth' first")
	}
	if count <= 0 || count > 100 {
		count = 30
	}
	qid := defaultHomeTimelineQueryID
	if value := os.Getenv("XEET_HOMETIMELINE_QID"); value != "" {
		qid = value
	}

	res, err := c.doHomeTimeline(ctx, qid, cursor, count)
	if err != nil {
		return nil, err
	}
	if needsQueryIDRefresh(res) {
		fresh, discoverErr := c.discoverOperation(ctx, "HomeTimeline")
		if discoverErr != nil {
			return nil, fmt.Errorf("home timeline endpoint changed and discovery failed: %w", discoverErr)
		}
		res, err = c.doHomeTimeline(ctx, fresh, cursor, count)
		if err != nil {
			return nil, err
		}
	}
	if err := statusToError(res.status, res.header); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("x returned a malformed timeline response")
	}
	if err := graphQLError(payload); err != nil {
		return nil, err
	}
	page := parseTimeline(payload)
	return &page, nil
}

// doHomeTimeline is a read, so transient failures are retried.
func (c *WebClient) doHomeTimeline(ctx context.Context, qid, cursor string, count int) (*httpResult, error) {
	variables := map[string]any{
		"count":                  count,
		"includePromotedContent": true,
		"latestControlAvailable": true,
		"requestContext":         "launch",
		"seenTweetIds":           []string{},
		"withCommunity":          true,
	}
	if cursor != "" {
		variables["cursor"] = cursor
		variables["requestContext"] = "scroll"
	}
	variablesJSON, _ := json.Marshal(variables)
	featuresJSON, _ := json.Marshal(timelineFeatures)
	fieldTogglesJSON, _ := json.Marshal(timelineFieldToggles)

	params := url.Values{}
	params.Set("variables", string(variablesJSON))
	params.Set("features", string(featuresJSON))
	params.Set("fieldToggles", string(fieldTogglesJSON))
	endpoint := fmt.Sprintf("https://x.com/i/api/graphql/%s/HomeTimeline?%s", qid, params.Encode())
	return c.send(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		return req, nil
	}, true, 20<<20)
}

func parseTimeline(payload any) TimelinePage {
	page := TimelinePage{}
	seen := map[string]bool{}
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if cursorType, _ := value["cursorType"].(string); strings.EqualFold(cursorType, "Bottom") {
				if cursor, _ := value["value"].(string); cursor != "" {
					page.Cursor = cursor
				}
			}
			if item, ok := value["itemContent"].(map[string]any); ok {
				if post, ok := parseTimelineItem(item); ok && !seen[post.ID] {
					seen[post.ID] = true
					page.Posts = append(page.Posts, post)
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

func parseTimelineItem(item map[string]any) (TimelinePost, bool) {
	if _, promoted := item["promoted_metadata"]; promoted {
		return TimelinePost{}, false
	}
	tweetResults, _ := item["tweet_results"].(map[string]any)
	result, _ := tweetResults["result"].(map[string]any)
	if nested, ok := result["tweet"].(map[string]any); ok {
		result = nested
	}
	id, _ := result["rest_id"].(string)
	legacy, _ := result["legacy"].(map[string]any)
	text, _ := legacy["full_text"].(string)
	if note, ok := result["note_tweet"].(map[string]any); ok {
		if noteResults, ok := note["note_tweet_results"].(map[string]any); ok {
			if noteResult, ok := noteResults["result"].(map[string]any); ok {
				if longText, ok := noteResult["text"].(string); ok && longText != "" {
					text = longText
				}
			}
		}
	}
	if id == "" || text == "" {
		return TimelinePost{}, false
	}
	text = html.UnescapeString(text)

	post := TimelinePost{ID: id, Text: text}
	post.ReplyCount = intValue(legacy["reply_count"])
	post.RepostCount = intValue(legacy["retweet_count"])
	post.LikeCount = intValue(legacy["favorite_count"])
	post.Liked, _ = legacy["favorited"].(bool)
	if created, _ := legacy["created_at"].(string); created != "" {
		post.CreatedAt, _ = time.Parse("Mon Jan 02 15:04:05 -0700 2006", created)
	}
	if entities, ok := legacy["extended_entities"].(map[string]any); ok {
		if media, ok := entities["media"].([]any); ok {
			for _, raw := range media {
				item, _ := raw.(map[string]any)
				url, _ := item["media_url_https"].(string)
				if url == "" {
					url, _ = item["media_url"].(string)
				}
				if url == "" {
					continue
				}
				entry := TimelineMedia{URL: url}
				entry.Type, _ = item["type"].(string)
				entry.AltText, _ = item["ext_alt_text"].(string)
				if original, ok := item["original_info"].(map[string]any); ok {
					entry.Width = intValue(original["width"])
					entry.Height = intValue(original["height"])
				}
				post.Media = append(post.Media, entry)
			}
			post.MediaCount = len(post.Media)
		}
	}
	if views, ok := result["views"].(map[string]any); ok {
		post.ViewCount, _ = views["count"].(string)
	}

	core, _ := result["core"].(map[string]any)
	userResults, _ := core["user_results"].(map[string]any)
	user, _ := userResults["result"].(map[string]any)
	userCore, _ := user["core"].(map[string]any)
	userLegacy, _ := user["legacy"].(map[string]any)
	post.AuthorName, _ = userCore["name"].(string)
	post.Handle, _ = userCore["screen_name"].(string)
	if post.AuthorName == "" {
		post.AuthorName, _ = userLegacy["name"].(string)
	}
	if post.Handle == "" {
		post.Handle, _ = userLegacy["screen_name"].(string)
	}
	return post, true
}

func intValue(value any) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case json.Number:
		result, _ := number.Int64()
		return int(result)
	}
	return 0
}
