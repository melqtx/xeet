package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
)

// Profile is the public author information returned by X's user lookup.
type Profile struct {
	Account
	Bio, Location, Website, Joined       string
	Followers, Following, Posts          int
	Protected                            bool
	YouFollow, FollowsYou                bool
	HasFollowers, HasFollowing, HasPosts bool
}

func (c *WebClient) FetchProfile(ctx context.Context, handle string) (*Profile, error) {
	handle = strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if handle == "" {
		return nil, fmt.Errorf("profile handle is empty")
	}
	if c.authToken == "" || c.ct0 == "" {
		return nil, fmt.Errorf("no session; run 'xeet auth' first")
	}
	qid := c.operationQIDs["UserByScreenName"]
	if qid == "" {
		var err error
		qid, err = c.discoverOperation(ctx, "UserByScreenName")
		if err != nil {
			return nil, err
		}
	}
	vars := map[string]any{"screen_name": handle, "withSafetyModeUserFields": true}
	res, err := c.doTimelineOp(ctx, "UserByScreenName", qid, vars, false)
	if err != nil {
		return nil, err
	}
	if needsQueryIDRefresh(res) {
		qid, err = c.discoverOperation(ctx, "UserByScreenName")
		if err != nil {
			return nil, err
		}
		res, err = c.doTimelineOp(ctx, "UserByScreenName", qid, vars, false)
		if err != nil {
			return nil, err
		}
	}
	if err := statusToError(res.status, res.header); err != nil {
		return nil, err
	}
	if res.status != http.StatusOK {
		return nil, fmt.Errorf("profile request failed (HTTP %d)", res.status)
	}
	var root map[string]any
	if err := json.Unmarshal(res.body, &root); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	if err := graphQLError(root); err != nil {
		return nil, err
	}
	data, _ := root["data"].(map[string]any)
	user, _ := data["user"].(map[string]any)
	result, _ := user["result"].(map[string]any)
	return parseProfileResult(result)
}

func parseProfileResult(result map[string]any) (*Profile, error) {
	account, ok := findViewerAccount(result)
	if !ok || account.ID == "" {
		return nil, fmt.Errorf("profile unavailable; this account may be suspended or deleted")
	}
	legacy, _ := result["legacy"].(map[string]any)
	str := func(key string) string { s, _ := legacy[key].(string); return html.UnescapeString(s) }
	p := &Profile{Account: account, Bio: str("description"), Location: str("location"), Website: str("url"), Joined: str("created_at"), Followers: intValue(legacy["followers_count"]), Following: intValue(legacy["friends_count"]), Posts: intValue(legacy["statuses_count"])}
	// New responses split public fields into dedicated objects; legacy remains
	// a fallback for older payloads. Presence matters: a real zero is valid.
	object := func(key string) map[string]any { v, _ := result[key].(map[string]any); return v }
	count := func(modern map[string]any, key, old string) (int, bool) {
		v, ok := modern[key]
		if !ok || v == nil {
			v, ok = legacy[old]
		}
		if !ok || v == nil {
			return 0, false
		}
		return intValue(v), true
	}
	p.Followers, p.HasFollowers = count(object("relationship_counts"), "followers", "followers_count")
	p.Following, p.HasFollowing = count(object("relationship_counts"), "following", "friends_count")
	p.Posts, p.HasPosts = count(object("tweet_counts"), "tweets", "statuses_count")
	bio := object("profile_bio")
	if description, ok := bio["description"].(string); ok {
		p.Bio = html.UnescapeString(description)
	}
	if website, ok := object("website")["url"].(string); ok {
		p.Website = website
	}
	p.YouFollow, _ = legacy["following"].(bool)
	p.FollowsYou, _ = legacy["followed_by"].(bool)
	p.Protected, _ = legacy["protected"].(bool)
	if protected, ok := object("privacy")["protected"].(bool); ok {
		p.Protected = protected
	}
	if verified, _ := object("verification")["verified"].(bool); verified {
		p.Verified = true
	}
	core, _ := result["core"].(map[string]any)
	if p.Joined == "" {
		p.Joined, _ = core["created_at"].(string)
	}
	location, _ := result["location"].(map[string]any)
	if p.Location == "" {
		p.Location, _ = location["location"].(string)
	}
	entities, _ := legacy["entities"].(map[string]any)
	if current, ok := bio["entities"].(map[string]any); ok {
		entities = current
	}
	link, _ := entities["url"].(map[string]any)
	urls, _ := link["urls"].([]any)
	if len(urls) > 0 {
		u, _ := urls[0].(map[string]any)
		if expanded, _ := u["expanded_url"].(string); expanded != "" {
			p.Website = expanded
		}
	}
	return p, nil
}

func (c *WebClient) FetchProfilePosts(ctx context.Context, userID, cursor string, count int) (*TimelinePage, error) {
	if userID == "" {
		return nil, fmt.Errorf("profile user id is empty")
	}
	return c.fetchTimelineOp(ctx, "UserTweets", "", "XEET_USERTWEETS_QID", count, false, func(count int) map[string]any {
		vars := map[string]any{"userId": userID, "count": count, "includePromotedContent": false, "withQuickPromoteEligibilityTweetFields": false, "withVoice": true}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		return vars
	})
}
