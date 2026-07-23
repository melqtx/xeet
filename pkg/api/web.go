package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"time"

	"xeet/pkg/config"
)

// webBearer is the public bearer token the x.com web app ships to every
// browser. It is not a secret and is the same for all logged-out and
// logged-in web sessions; the actual authentication comes from the cookies.
const webBearer = "AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"

// createTweetQueryID is the GraphQL persisted-query id for CreateTweet. X
// rotates these periodically; override with XEET_CREATETWEET_QID if posting
// starts returning 404 from the GraphQL endpoint.
const defaultCreateTweetQueryID = "znk3sQMwOEHIDfrHvCK7yQ"

// loginUA is the browser User-Agent used for x.com/CDN requests.
const loginUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// LoginResult is the session material an auth method yields.
type LoginResult struct {
	AuthToken string
	CT0       string
}

// Upload is one image to upload and attach to a post.
type Upload struct {
	Filename    string
	ContentType string
	Data        []byte
}

type PostStage string

const (
	PostStageUploading   PostStage = "uploading"
	PostStageDiscovering PostStage = "discovering"
	PostStagePublishing  PostStage = "publishing"
	PostStageComplete    PostStage = "complete"
)

// PostEvent reports coarse posting progress. The callback is optional.
type PostEvent struct {
	Stage   PostStage
	Current int
	Total   int
	Name    string
}

type ProgressFunc func(PostEvent)

// WebClient posts through x.com's internal GraphQL API using a logged-in
// browser session (auth_token + ct0 cookies) instead of the developer API.
type WebClient struct {
	httpClient *http.Client
	authToken  string
	ct0        string
	queryID    string
	refreshed  bool // queryID was re-discovered this session and should be cached
}

func NewWebClient(cfg *config.Config) *WebClient {
	// Prefer an explicit override, then the cached id, then a built-in default
	// (which may be stale — PostTweet re-discovers automatically on a 404).
	qid := defaultCreateTweetQueryID
	if cfg.CreateTweetQID != "" {
		qid = cfg.CreateTweetQID
	}
	if v := os.Getenv("XEET_CREATETWEET_QID"); v != "" {
		qid = v
	}
	return &WebClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		authToken:  cfg.AuthToken,
		ct0:        cfg.CT0,
		queryID:    qid,
	}
}

// QueryID returns the CreateTweet query id currently in use (possibly one that
// was auto-discovered this session).
func (c *WebClient) QueryID() string { return c.queryID }

// Refreshed reports whether the query id was re-discovered this session, so the
// caller knows to persist QueryID() back to the config.
func (c *WebClient) Refreshed() bool { return c.refreshed }

// setHeaders applies the header set the web client sends on every
// authenticated GraphQL request.
func (c *WebClient) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+webBearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", fmt.Sprintf("auth_token=%s; ct0=%s", c.authToken, c.ct0))
	req.Header.Set("X-Csrf-Token", c.ct0)
	req.Header.Set("X-Twitter-Auth-Type", "OAuth2Session")
	req.Header.Set("X-Twitter-Active-User", "yes")
	req.Header.Set("X-Twitter-Client-Language", "en")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://x.com/home")
	req.Header.Set("Origin", "https://x.com")
}

// createTweetFeatures is the feature flag set CreateTweet requires. Missing
// keys cause the endpoint to reject the request, so this mirrors what the web
// client sends.
var createTweetFeatures = map[string]bool{
	"communities_web_enable_tweet_community_results_fetch":                    true,
	"c9s_tweet_anatomy_moderator_badge_enabled":                               true,
	"responsive_web_grok_analyze_button_fetch_trends_enabled":                 false,
	"responsive_web_grok_analyze_post_followups_enabled":                      true,
	"responsive_web_jetfuel_frame":                                            false,
	"responsive_web_grok_share_attachment_enabled":                            true,
	"tweetypie_unmention_optimization_enabled":                                true,
	"responsive_web_edit_tweet_api_enabled":                                   true,
	"graphql_is_translatable_rweb_tweet_is_translatable_enabled":              true,
	"view_counts_everywhere_api_enabled":                                      true,
	"longform_notetweets_consumption_enabled":                                 true,
	"responsive_web_twitter_article_tweet_consumption_enabled":                true,
	"tweet_awards_web_tipping_enabled":                                        false,
	"responsive_web_grok_analysis_button_from_backend":                        true,
	"creator_subscriptions_quote_tweet_preview_enabled":                       false,
	"longform_notetweets_rich_text_read_enabled":                              true,
	"longform_notetweets_inline_media_enabled":                                true,
	"profile_label_improvements_pcf_label_in_post_enabled":                    true,
	"rweb_tipjar_consumption_enabled":                                         true,
	"responsive_web_graphql_exclude_directive_enabled":                        true,
	"verified_phone_label_enabled":                                            false,
	"articles_preview_enabled":                                                true,
	"rweb_video_timestamps_enabled":                                           true,
	"responsive_web_graphql_skip_user_profile_image_extensions_enabled":       false,
	"freedom_of_speech_not_reach_fetch_enabled":                               true,
	"standardized_nudges_misinfo":                                             true,
	"tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled": true,
	"responsive_web_grok_image_annotation_enabled":                            true,
	"responsive_web_graphql_timeline_navigation_enabled":                      true,
	"responsive_web_enhance_cards_enabled":                                    false,
}

type createTweetVariables struct {
	TweetText             string   `json:"tweet_text"`
	DarkRequest           bool     `json:"dark_request"`
	Media                 ctMedia  `json:"media"`
	SemanticAnnotationIDs []string `json:"semantic_annotation_ids"`
	Reply                 *ctReply `json:"reply,omitempty"`
}

type ctMedia struct {
	MediaEntities     []mediaEntity `json:"media_entities"`
	PossiblySensitive bool          `json:"possibly_sensitive"`
}

type mediaEntity struct {
	MediaID     string   `json:"media_id"`
	TaggedUsers []string `json:"tagged_users"`
}

type ctReply struct {
	InReplyToTweetID    string   `json:"in_reply_to_tweet_id"`
	ExcludeReplyUserIDs []string `json:"exclude_reply_user_ids"`
}

// PostTweet posts through the web GraphQL endpoint and returns the created id.
// Images are uploaded first and attached to the same CreateTweet operation.
func (c *WebClient) PostTweet(ctx context.Context, text, replyToID string, uploads []Upload, progress ProgressFunc) (string, error) {
	if c.authToken == "" || c.ct0 == "" {
		return "", fmt.Errorf("no session — run 'xeet auth' first")
	}
	if len(uploads) > 4 {
		return "", fmt.Errorf("a post can have at most 4 images")
	}
	if text == "" && len(uploads) == 0 {
		return "", fmt.Errorf("post has no text or images")
	}
	for i, upload := range uploads {
		if len(upload.Data) == 0 {
			return "", fmt.Errorf("image %d is empty", i+1)
		}
	}
	vars := createTweetVariables{
		TweetText:             text,
		DarkRequest:           false,
		Media:                 ctMedia{MediaEntities: []mediaEntity{}, PossiblySensitive: false},
		SemanticAnnotationIDs: []string{},
	}
	for i, upload := range uploads {
		emitProgress(progress, PostEvent{Stage: PostStageUploading, Current: i + 1, Total: len(uploads), Name: upload.Filename})
		mediaID, err := c.uploadMedia(ctx, upload)
		if err != nil {
			return "", fmt.Errorf("upload %q: %w", upload.Filename, err)
		}
		vars.Media.MediaEntities = append(vars.Media.MediaEntities, mediaEntity{MediaID: mediaID, TaggedUsers: []string{}})
	}
	if replyToID != "" {
		vars.Reply = &ctReply{InReplyToTweetID: replyToID, ExcludeReplyUserIDs: []string{}}
	}

	emitProgress(progress, PostEvent{Stage: PostStagePublishing})
	status, respBody, err := c.doCreateTweet(ctx, vars, c.queryID)
	if err != nil {
		return "", err
	}

	// A 404 means the persisted-query id rotated. Discover the current one from
	// X's live JS bundles, cache it, and retry once.
	if status == http.StatusNotFound {
		emitProgress(progress, PostEvent{Stage: PostStageDiscovering})
		fresh, derr := DiscoverCreateTweetQueryID(ctx, c.authToken, c.ct0)
		if derr != nil {
			return "", fmt.Errorf("the CreateTweet endpoint id is stale and auto-discovery failed (%v).\n"+
				"Grab it manually: open x.com, post a tweet, in DevTools > Network find the 'CreateTweet' request,\n"+
				"copy the id in its URL, then run:  xeet setqid <id>", derr)
		}
		c.queryID = fresh
		c.refreshed = true
		status, respBody, err = c.doCreateTweet(ctx, vars, fresh)
		if err != nil {
			return "", err
		}
	}

	if status == http.StatusForbidden || status == http.StatusUnauthorized {
		return "", fmt.Errorf("session rejected (HTTP %d) — your cookies may have expired, re-run 'xeet auth'. Response: %s", status, truncate(respBody))
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("web API error %d: %s", status, truncate(respBody))
	}

	// GraphQL returns 200 even for logical errors, in an "errors" array.
	var parsed struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Data struct {
			CreateTweet struct {
				TweetResults struct {
					Result struct {
						RestID string `json:"rest_id"`
					} `json:"result"`
				} `json:"tweet_results"`
			} `json:"create_tweet"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode response: %w (body: %s)", err, truncate(respBody))
	}
	if len(parsed.Errors) > 0 {
		return "", fmt.Errorf("x graphql error: %s", parsed.Errors[0].Message)
	}

	emitProgress(progress, PostEvent{Stage: PostStageComplete})
	return parsed.Data.CreateTweet.TweetResults.Result.RestID, nil
}

func emitProgress(progress ProgressFunc, event PostEvent) {
	if progress != nil {
		progress(event)
	}
}

// doCreateTweet issues one CreateTweet GraphQL request with the given query id
// and returns the HTTP status and body.
func (c *WebClient) doCreateTweet(ctx context.Context, vars createTweetVariables, queryID string) (int, []byte, error) {
	payload := map[string]any{
		"variables": vars,
		"features":  createTweetFeatures,
		"queryId":   queryID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal: %w", err)
	}

	endpoint := fmt.Sprintf("https://x.com/i/api/graphql/%s/CreateTweet", queryID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}

// uploadMedia does a simple (non-chunked) image upload through the cookie
// session and returns the media id to attach to a tweet.
func (c *WebClient) uploadMedia(ctx context.Context, upload Upload) (string, error) {
	if len(upload.Data) == 0 {
		return "", fmt.Errorf("empty image")
	}
	filename := filepath.Base(upload.Filename)
	if filename == "." || filename == "" {
		filename = "image"
	}
	contentType := upload.ContentType
	if contentType == "" {
		contentType = http.DetectContentType(upload.Data)
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", multipart.FileContentDisposition("media", filename))
	header.Set("Content-Type", contentType)
	part, err := w.CreatePart(header)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(upload.Data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://upload.twitter.com/1.1/media/upload.json", &buf)
	if err != nil {
		return "", err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", w.FormDataContentType()) // override the JSON default

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, truncate(body))
	}

	var out struct {
		MediaIDString string `json:"media_id_string"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	if out.MediaIDString == "" {
		return "", fmt.Errorf("upload returned no media id: %s", truncate(body))
	}
	return out.MediaIDString, nil
}

// Verify confirms the session cookies work with a cheap authenticated GET that
// echoes the account handle. It returns the screen name on success. The web
// client reaches 1.1 endpoints through the same-origin x.com/i/api proxy; the
// bare api.x.com host 404s several of them, so try the proxy first.
func (c *WebClient) Verify(ctx context.Context) (string, error) {
	if c.authToken == "" || c.ct0 == "" {
		return "", fmt.Errorf("web session not configured")
	}

	endpoints := []string{
		"https://x.com/i/api/1.1/account/settings.json",
		"https://api.x.com/1.1/account/settings.json",
		"https://api.twitter.com/1.1/account/settings.json",
	}

	var lastErr error
	for _, ep := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep, nil)
		if err != nil {
			lastErr = err
			continue
		}
		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, ep, truncate(respBody))
			continue
		}
		var settings struct {
			ScreenName string `json:"screen_name"`
		}
		if err := json.Unmarshal(respBody, &settings); err != nil {
			lastErr = fmt.Errorf("decode settings from %s: %w", ep, err)
			continue
		}
		return settings.ScreenName, nil
	}
	return "", lastErr
}

func truncate(b []byte) string {
	const max = 400
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
