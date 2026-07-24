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
	"runtime"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

// webBearer is the public bearer token the x.com web app ships to every
// browser. It is not a secret and is the same for all logged-out and
// logged-in web sessions; the actual authentication comes from the cookies.
const webBearer = "AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"

// createTweetQueryID is the GraphQL persisted-query id for CreateTweet. X
// rotates these periodically; override with XEET_CREATETWEET_QID if posting
// starts returning 404 from the GraphQL endpoint.
const defaultCreateTweetQueryID = "hIL9XdleMYEtVXOZVbr8Bg"

// loginUA is the browser User-Agent used for x.com/CDN requests. It must stay
// consistent with the Sec-Ch-Ua client hints sent on CreateTweet: a mismatched
// pair is a bot-detection signal.
const loginUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"

// LoginResult is the session material an auth method yields.
type LoginResult struct {
	AuthToken    string
	CT0          string
	Profile      string
	CookieDomain string
	ExpiresAt    time.Time
	LastUsedAt   time.Time
}

// Upload is one media item to upload and attach to a post. Images travel in
// Data; large media (video) sets Path instead and streams from disk through
// the chunked endpoint.
type Upload struct {
	Filename    string
	ContentType string
	Data        []byte
	Path        string
}

type PostStage string

const (
	PostStageUploading   PostStage = "uploading"
	PostStageProcessing  PostStage = "processing"
	PostStageDiscovering PostStage = "discovering"
	PostStagePublishing  PostStage = "publishing"
	PostStageReconciling PostStage = "reconciling"
	PostStageComplete    PostStage = "complete"
)

// PostEvent reports coarse posting progress. The callback is optional.
// Chunked uploads additionally fill the byte counters so UIs can show a
// percentage while a video transfers.
type PostEvent struct {
	Stage            PostStage
	Current          int
	Total            int
	Name             string
	TransferredBytes int64
	TotalBytes       int64
}

type ProgressFunc func(PostEvent)

// WebClient posts through x.com's internal GraphQL API using a logged-in
// browser session (auth_token + ct0 cookies) instead of the developer API.
type WebClient struct {
	httpClient          *http.Client
	authToken           string
	ct0                 string
	operationQIDs       map[string]string
	refreshedOperations map[string]string
	retryDelay          time.Duration // base backoff between transient retries; tests shrink it
	reconcileDelay      time.Duration
	lastDiagnostic      string
	userAgent           string
	clientPlatform      string
	transactionIDs      *transactionIDGenerator
	transactionID       func(context.Context, string, string) (string, error)
	discover            func(context.Context, string, string, string) (string, error)
}

// httpResult is one completed HTTP exchange: everything callers need to
// classify the outcome without holding the response open.
type httpResult struct {
	status int
	header http.Header
	body   []byte
}

// maxRequestAttempts bounds transient retries: 1 try + 2 retries.
const maxRequestAttempts = 3

// send issues the request produced by build. When retryTransient is set it
// retries network errors and gateway-flavored 5xx responses with a short
// exponential backoff, bounded by maxRequestAttempts and the context.
// Requests are rebuilt per attempt because bodies are consumed. Mutations
// that must not double-fire (CreateTweet) pass retryTransient=false.
func (c *WebClient) send(ctx context.Context, build func() (*http.Request, error), retryTransient bool, bodyLimit int64) (*httpResult, error) {
	delay := c.retryDelay
	if delay == 0 {
		delay = 500 * time.Millisecond
	}

	var lastErr error
	for attempt := 1; attempt <= maxRequestAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, classifyTransportError(ctx.Err())
			case <-time.After(delay):
			}
			delay *= 2
		}

		req, err := build()
		if err != nil {
			return nil, fmt.Errorf("request: %w", err)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if !retryTransient {
				return nil, classifyTransportError(err)
			}
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
		resp.Body.Close()

		if retryTransient && isTransientStatus(resp.StatusCode) && attempt < maxRequestAttempts {
			lastErr = fmt.Errorf("x returned HTTP %d", resp.StatusCode)
			continue
		}
		return &httpResult{status: resp.StatusCode, header: resp.Header, body: body}, nil
	}
	return nil, classifyTransportError(lastErr)
}

func isTransientStatus(status int) bool {
	switch status {
	case http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// needsQueryIDRefresh reports whether a response indicates the persisted-query
// id rotated: a plain 404, or a 400 that names the persisted query problem.
func needsQueryIDRefresh(res *httpResult) bool {
	if res.status == http.StatusNotFound {
		return true
	}
	return res.status == http.StatusBadRequest &&
		(bytes.Contains(res.body, []byte("PersistedQueryNotFound")) || bytes.Contains(res.body, []byte("queryId")))
}

func NewWebClient(cfg *config.Config) *WebClient {
	operationQIDs := map[string]string{
		"CreateTweet":        cfg.CreateTweetQID,
		"HomeTimeline":       cfg.HomeTimelineQID,
		"HomeLatestTimeline": cfg.HomeLatestTimelineQID,
		"FavoriteTweet":      cfg.FavoriteTweetQID,
		"UnfavoriteTweet":    cfg.UnfavoriteTweetQID,
		"Viewer":             cfg.ViewerQID,
		"TweetDetail":        cfg.TweetDetailQID,
	}
	qid := operationQIDs["CreateTweet"]
	if qid == "" {
		qid = defaultCreateTweetQueryID
	}
	if v := os.Getenv("XEET_CREATETWEET_QID"); v != "" {
		qid = v
	}
	operationQIDs["CreateTweet"] = qid
	client := &WebClient{
		httpClient:          &http.Client{Timeout: 30 * time.Second},
		authToken:           cfg.AuthToken,
		ct0:                 cfg.CT0,
		operationQIDs:       operationQIDs,
		refreshedOperations: map[string]string{},
		reconcileDelay:      1500 * time.Millisecond,
		userAgent:           chromiumUserAgent(runtime.GOOS),
		clientPlatform:      chromiumClientPlatform(runtime.GOOS),
	}
	client.transactionID = client.generateTransactionID
	return client
}

// createTweetQueryID is the CreateTweet id currently in use: the cached or
// default one, or a fresher id discovered after a rotation this session.
func (c *WebClient) createTweetQueryID() string {
	return c.operationQIDs["CreateTweet"]
}

func (c *WebClient) discoverOperation(ctx context.Context, operation string) (string, error) {
	var (
		qid string
		err error
	)
	if c.discover != nil {
		qid, err = c.discover(ctx, c.authToken, c.ct0, operation)
	} else {
		qid, err = DiscoverOperationQueryID(ctx, c.authToken, c.ct0, operation)
	}
	if err != nil {
		return "", err
	}
	if c.operationQIDs == nil {
		c.operationQIDs = map[string]string{}
	}
	if c.refreshedOperations == nil {
		c.refreshedOperations = map[string]string{}
	}
	c.operationQIDs[operation] = qid
	c.refreshedOperations[operation] = qid
	return qid, nil
}

// ApplyRefreshedQueryIDs copies operation ids discovered by this client into
// cfg. It reports whether there is anything for the caller to persist.
func (c *WebClient) ApplyRefreshedQueryIDs(cfg *config.Config) bool {
	if cfg == nil || len(c.refreshedOperations) == 0 {
		return false
	}
	for operation, qid := range c.refreshedOperations {
		switch operation {
		case "CreateTweet":
			cfg.CreateTweetQID = qid
		case "HomeTimeline":
			cfg.HomeTimelineQID = qid
		case "HomeLatestTimeline":
			cfg.HomeLatestTimelineQID = qid
		case "FavoriteTweet":
			cfg.FavoriteTweetQID = qid
		case "UnfavoriteTweet":
			cfg.UnfavoriteTweetQID = qid
		case "Viewer":
			cfg.ViewerQID = qid
		case "TweetDetail":
			cfg.TweetDetailQID = qid
		}
	}
	return true
}

func (c *WebClient) operationQueryID(operation, fallback, environment string) string {
	qid := c.operationQIDs[operation]
	if qid == "" {
		qid = fallback
	}
	if environment != "" {
		if override := os.Getenv(environment); override != "" {
			qid = override
		}
	}
	return qid
}

// LastDiagnostic returns sanitized response-shape metadata from the most recent
// CreateTweet call. It never includes cookies, request bodies, or post text.
func (c *WebClient) LastDiagnostic() string { return c.lastDiagnostic }

func chromiumUserAgent(goos string) string {
	if goos == "linux" {
		return "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"
	}
	if goos == "windows" {
		return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"
	}
	return loginUA
}

func chromiumClientPlatform(goos string) string {
	switch goos {
	case "linux":
		return `"Linux"`
	case "windows":
		return `"Windows"`
	default:
		return `"macOS"`
	}
}

func (c *WebClient) browserUserAgent() string {
	if c.userAgent != "" {
		return c.userAgent
	}
	return loginUA
}

func (c *WebClient) browserClientPlatform() string {
	if c.clientPlatform != "" {
		return c.clientPlatform
	}
	return `"macOS"`
}

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
	req.Header.Set("User-Agent", c.browserUserAgent())
	req.Header.Set("Referer", "https://x.com/home")
	req.Header.Set("Origin", "https://x.com")
}

// setCreateTweetBrowserHeaders applies the captured browser request metadata
// that does not vary per request.
func (c *WebClient) setCreateTweetBrowserHeaders(req *http.Request) {
	c.setHeaders(req)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Priority", "u=1, i")
	req.Header.Set("Referer", "https://x.com/compose/post")
	req.Header.Set("Sec-Ch-Ua", `"Not;A=Brand";v="8", "Chromium";v="150", "Google Chrome";v="150"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", c.browserClientPlatform())
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
}

// setCreateTweetHeaders adds X's per-request transaction proof. A captured
// transaction id cannot be replayed because it includes the method, path,
// current time, and X's rotating browser animation key.
func (c *WebClient) setCreateTweetHeaders(req *http.Request) error {
	c.setCreateTweetBrowserHeaders(req)
	// Tests and custom clients can omit the generator; production clients
	// created by NewWebClient always install it.
	if c.transactionID == nil {
		return nil
	}
	transactionID, err := c.transactionID(req.Context(), req.Method, req.URL.Path)
	if err != nil {
		return fmt.Errorf("generate X transaction id: %w", err)
	}
	req.Header.Set("X-Client-Transaction-Id", transactionID)
	return nil
}

// createTweetFeatures is the feature flag set CreateTweet requires. Missing
// keys cause the endpoint to reject the request, so this mirrors what the web
// client sends.
var createTweetFeatures = map[string]bool{
	"premium_content_api_read_enabled":                                        false,
	"communities_web_enable_tweet_community_results_fetch":                    true,
	"c9s_tweet_anatomy_moderator_badge_enabled":                               true,
	"responsive_web_grok_analyze_button_fetch_trends_enabled":                 false,
	"responsive_web_grok_analyze_post_followups_enabled":                      true,
	"rweb_cashtags_composer_attachment_enabled":                               true,
	"responsive_web_jetfuel_frame":                                            true,
	"responsive_web_grok_share_attachment_enabled":                            true,
	"responsive_web_grok_annotations_enabled":                                 true,
	"responsive_web_edit_tweet_api_enabled":                                   true,
	"rweb_conversational_replies_downvote_enabled":                            false,
	"graphql_is_translatable_rweb_tweet_is_translatable_enabled":              true,
	"view_counts_everywhere_api_enabled":                                      true,
	"longform_notetweets_consumption_enabled":                                 true,
	"responsive_web_twitter_article_tweet_consumption_enabled":                true,
	"content_disclosure_indicator_enabled":                                    true,
	"content_disclosure_ai_generated_indicator_enabled":                       true,
	"responsive_web_grok_show_grok_translated_post":                           true,
	"responsive_web_grok_analysis_button_from_backend":                        true,
	"post_ctas_fetch_enabled":                                                 false,
	"longform_notetweets_rich_text_read_enabled":                              true,
	"longform_notetweets_inline_media_enabled":                                false,
	"profile_label_improvements_pcf_label_in_post_enabled":                    true,
	"responsive_web_profile_redirect_enabled":                                 true,
	"rweb_tipjar_consumption_enabled":                                         false,
	"verified_phone_label_enabled":                                            true,
	"articles_preview_enabled":                                                true,
	"rweb_cashtags_enabled":                                                   true,
	"responsive_web_grok_community_note_auto_translation_is_enabled":          true,
	"responsive_web_graphql_skip_user_profile_image_extensions_enabled":       false,
	"freedom_of_speech_not_reach_fetch_enabled":                               true,
	"standardized_nudges_misinfo":                                             true,
	"tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled": true,
	"responsive_web_grok_image_annotation_enabled":                            true,
	"responsive_web_grok_imagine_annotation_enabled":                          true,
	"responsive_web_graphql_timeline_navigation_enabled":                      true,
}

type createTweetVariables struct {
	TweetText                 string                      `json:"tweet_text"`
	Media                     ctMedia                     `json:"media"`
	SemanticAnnotationIDs     []string                    `json:"semantic_annotation_ids"`
	DisallowedReplyOptions    []string                    `json:"disallowed_reply_options"`
	SemanticAnnotationOptions ctSemanticAnnotationOptions `json:"semantic_annotation_options"`
	Reply                     *ctReply                    `json:"reply,omitempty"`
}

type ctSemanticAnnotationOptions struct {
	Source string `json:"source"`
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

func newCreateTweetVariables(text string) createTweetVariables {
	return createTweetVariables{
		TweetText:                 text,
		Media:                     ctMedia{MediaEntities: []mediaEntity{}, PossiblySensitive: false},
		SemanticAnnotationIDs:     []string{},
		DisallowedReplyOptions:    nil,
		SemanticAnnotationOptions: ctSemanticAnnotationOptions{Source: "Htl"},
	}
}

// PostTweet posts through the web GraphQL endpoint and returns the created id.
// Media is uploaded first and attached to the same CreateTweet operation.
func (c *WebClient) PostTweet(ctx context.Context, text, replyToID string, uploads []Upload, progress ProgressFunc) (string, error) {
	c.lastDiagnostic = ""
	if c.authToken == "" || c.ct0 == "" {
		return "", fmt.Errorf("no session; run 'xeet auth' first")
	}
	if len(uploads) > 4 {
		return "", fmt.Errorf("a post can have at most 4 attachments")
	}
	if text == "" && len(uploads) == 0 {
		return "", fmt.Errorf("post has no text or media")
	}
	videos := 0
	for i, upload := range uploads {
		if len(upload.Data) == 0 && upload.Path == "" {
			return "", fmt.Errorf("attachment %d is empty", i+1)
		}
		if upload.isVideo() {
			videos++
		}
	}
	if videos > 0 && len(uploads) > 1 {
		return "", fmt.Errorf("a video must be the only attachment")
	}
	vars := newCreateTweetVariables(text)
	for i, upload := range uploads {
		emitProgress(progress, PostEvent{Stage: PostStageUploading, Current: i + 1, Total: len(uploads), Name: upload.Filename})
		var mediaID string
		var err error
		if upload.needsChunkedUpload() {
			mediaID, err = c.uploadMediaChunked(ctx, upload, func(sent, total int64) {
				stage := PostStageUploading
				if sent == total {
					// The transfer is done; anything after this is X transcoding.
					stage = PostStageProcessing
				}
				emitProgress(progress, PostEvent{
					Stage: stage, Current: i + 1, Total: len(uploads), Name: upload.Filename,
					TransferredBytes: sent, TotalBytes: total,
				})
			})
		} else {
			mediaID, err = c.uploadMedia(ctx, upload)
		}
		if err != nil {
			return "", fmt.Errorf("upload %q: %w", upload.Filename, err)
		}
		vars.Media.MediaEntities = append(vars.Media.MediaEntities, mediaEntity{MediaID: mediaID, TaggedUsers: []string{}})
	}
	if replyToID != "" {
		vars.Reply = &ctReply{InReplyToTweetID: replyToID, ExcludeReplyUserIDs: []string{}}
	}

	emitProgress(progress, PostEvent{Stage: PostStagePublishing})
	if err := ctx.Err(); err != nil {
		return "", err
	}
	attemptedAt := time.Now()
	res, err := c.doCreateTweet(ctx, vars, c.createTweetQueryID())
	if err != nil {
		return c.finishAmbiguousCreate(
			ctx, text, replyToID, len(uploads), attemptedAt,
			createTransportDiagnostic(err), progress,
		)
	}

	// A 404 (or a 400 blaming the persisted query) means the query id rotated.
	// Discover the current one from X's live JS bundles, cache it, retry once.
	if needsQueryIDRefresh(res) {
		emitProgress(progress, PostEvent{Stage: PostStageDiscovering})
		fresh, derr := c.discoverOperation(ctx, "CreateTweet")
		if derr != nil {
			return "", fmt.Errorf("the CreateTweet endpoint id is stale and auto-discovery failed (%v).\n"+
				"Grab it manually: open x.com, post a tweet, in DevTools > Network find the 'CreateTweet' request,\n"+
				"copy the id in its URL, then run:  xeet setqid <id>", derr)
		}
		attemptedAt = time.Now()
		res, err = c.doCreateTweet(ctx, vars, fresh)
		if err != nil {
			return c.finishAmbiguousCreate(
				ctx, text, replyToID, len(uploads), attemptedAt,
				createTransportDiagnostic(err), progress,
			)
		}
	}

	outcome := parseCreateTweetResponse(res)
	c.lastDiagnostic = outcome.diagnostic
	if outcome.ambiguous {
		return c.finishAmbiguousCreate(
			ctx, text, replyToID, len(uploads), attemptedAt,
			outcome.diagnostic, progress,
		)
	}
	if outcome.err != nil {
		return "", outcome.err
	}

	emitProgress(progress, PostEvent{Stage: PostStageComplete})
	return outcome.id, nil
}

func emitProgress(progress ProgressFunc, event PostEvent) {
	if progress != nil {
		progress(event)
	}
}

// doCreateTweet issues one CreateTweet GraphQL request with the given query
// id. It is a mutation, so transient failures are never auto-retried: a
// timed-out request may still have posted, and retrying risks a double post.
func (c *WebClient) doCreateTweet(ctx context.Context, vars createTweetVariables, queryID string) (*httpResult, error) {
	payload := map[string]any{
		"variables": vars,
		"features":  createTweetFeatures,
		"queryId":   queryID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	endpoint := fmt.Sprintf("https://x.com/i/api/graphql/%s/CreateTweet", queryID)
	return c.send(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		if err := c.setCreateTweetHeaders(req); err != nil {
			return nil, err
		}
		return req, nil
	}, false, 4<<20)
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

	// Retrying an upload is safe: the worst case is an orphaned media id that
	// X garbage-collects, never a duplicate post.
	formBody := buf.Bytes()
	res, err := c.send(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"https://upload.twitter.com/1.1/media/upload.json", bytes.NewReader(formBody))
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		req.Header.Set("Content-Type", w.FormDataContentType()) // override the JSON default
		return req, nil
	}, true, 4<<20)
	if err != nil {
		return "", err
	}
	if err := statusToError(res.status, res.header); err != nil {
		return "", err
	}
	if isTransientStatus(res.status) {
		return "", &ServiceUnavailableError{Status: res.status}
	}
	if res.status != http.StatusOK && res.status != http.StatusCreated {
		return "", fmt.Errorf("upload HTTP %d: %s", res.status, truncate(res.body))
	}

	var out struct {
		MediaIDString string `json:"media_id_string"`
	}
	if err := json.Unmarshal(res.body, &out); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	if out.MediaIDString == "" {
		return "", fmt.Errorf("upload returned no media id: %s", truncate(res.body))
	}
	return out.MediaIDString, nil
}

// Verify confirms the session cookies with a non-mutating authenticated
// timeline read. Account identity is a separate question, answered by
// FetchViewer.
func (c *WebClient) Verify(ctx context.Context) error {
	if c.authToken == "" || c.ct0 == "" {
		return fmt.Errorf("web session not configured")
	}
	_, err := c.FetchHomeTimeline(ctx, "", 1)
	return err
}

func truncate(b []byte) string {
	const max = 400
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
