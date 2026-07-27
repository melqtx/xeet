package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

// X ships all its GraphQL persisted-query ids inside the public JavaScript
// bundles the web client loads. Those bundles are always current, so instead of
// hardcoding the rotating CreateTweet queryId we scrape it from the live JS.

var (
	scriptURLRe = regexp.MustCompile(`https://abs\.twimg\.com/responsive-web/client-web[a-zA-Z0-9._~/-]*?\.js`)
	queryIDRe   = regexp.MustCompile(`queryId:"([a-zA-Z0-9_-]{8,})"`)
	chunkPairRe = regexp.MustCompile(`([0-9]+):"([^"]+)"`)
	chunkHashRe = regexp.MustCompile(`^[0-9a-f]{7}$`)
)

// DiscoverCreateTweetQueryID fetches x.com's JS bundles and extracts the current
// CreateTweet persisted-query id. Cookies are optional; they only help the home
// page reference the composer's chunk. Returns an error if it can't be found.
func DiscoverCreateTweetQueryID(ctx context.Context, authToken, ct0 string) (string, error) {
	return DiscoverOperationQueryID(ctx, authToken, ct0, "CreateTweet")
}

// DiscoverOperationQueryID finds a current persisted-query id in X's live web
// bundles. It also reconstructs lazy webpack chunk URLs from the runtime map,
// which is necessary for route-specific operations such as HomeTimeline.
func DiscoverOperationQueryID(ctx context.Context, authToken, ct0, operation string) (string, error) {
	hc := &http.Client{Timeout: 25 * time.Second}

	// Load the home shell (authenticated when possible) to gather bundle URLs.
	html, err := fetchBody(ctx, hc, "https://x.com/home", authToken, ct0)
	if err != nil {
		html, err = fetchBody(ctx, hc, "https://x.com/", "", "")
		if err != nil {
			return "", fmt.Errorf("couldn't load x.com: %w", err)
		}
	}

	htmlText := string(html)
	urls := dedupe(scriptURLRe.FindAllString(htmlText, -1))
	urls = append(urls, webpackChunkURLs(htmlText, operation)...)

	// main.js references further bundles; pull it and harvest any URLs inside.
	var mainJS string
	for _, u := range urls {
		if strings.Contains(u, "/main.") {
			if b, e := fetchBody(ctx, hc, u, "", ""); e == nil {
				mainJS = string(b)
				urls = append(urls, scriptURLRe.FindAllString(mainJS, -1)...)
				urls = append(urls, webpackChunkURLs(mainJS, operation)...)
			}
			break
		}
	}
	urls = dedupe(urls)

	// Check main.js itself first, then the rest, tweet/api bundles prioritised.
	if id := findOperationQID(mainJS, operation); id != "" {
		return id, nil
	}
	sort.SliceStable(urls, func(i, j int) bool {
		return bundleScore(urls[i]) > bundleScore(urls[j])
	})
	for _, u := range urls {
		b, e := fetchBody(ctx, hc, u, "", "")
		if e != nil {
			continue
		}
		if id := findOperationQID(string(b), operation); id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("scanned %d bundles but found no %s operation", len(urls), operation)
}

// webpackChunkURLs rebuilds likely lazy chunk URLs from X's inline webpack
// name/hash maps. Runtime hashes currently receive a final "a" before .js.
func webpackChunkURLs(runtime, operation string) []string {
	names := map[string]string{}
	hashes := map[string]string{}
	for _, match := range chunkPairRe.FindAllStringSubmatch(runtime, -1) {
		if chunkHashRe.MatchString(match[2]) {
			hashes[match[1]] = match[2]
		} else if strings.Contains(strings.ToLower(match[2]), strings.ToLower(operationHint(operation))) {
			names[match[1]] = match[2]
		}
	}
	var urls []string
	for id, name := range names {
		if hash := hashes[id]; hash != "" {
			urls = append(urls, "https://abs.twimg.com/responsive-web/client-web/"+name+"."+hash+"a.js")
		}
	}
	return urls
}

func operationHint(operation string) string {
	if operation == "TweetDetail" {
		return "TweetDetail"
	}
	if strings.Contains(operation, "Home") {
		return "HomeTimeline"
	}
	if operation == "SearchTimeline" {
		return "SearchTimeline"
	}
	// The list read operations ship in the shared timeline chunk, which is named
	// for the bundles that pull it — Bookmarks, Explore, HomeTimeline — and never
	// for List. Hinting "List" only finds the list *management* bundles
	// (UserLists, ListHandler), none of which carry these operations.
	if operation == listLatestTweetsOperation || operation == listsManagementPageOperation {
		return "HomeTimeline"
	}
	if strings.Contains(operation, "Tweet") {
		return "Compose"
	}
	return operation
}

// findCreateTweetQID returns the queryId belonging to the CreateTweet operation
// descriptor. queryId and operationName live in the same minified object
// literal, but neighbouring operations' ids are nearby too, so we pick the
// queryId whose position is closest to a CreateTweet marker.
func findCreateTweetQID(js string) string {
	return findOperationQID(js, "CreateTweet")
}

func findOperationQID(js, operation string) string {
	marker := `operationName:"` + operation + `"`
	if !strings.Contains(js, marker) {
		return ""
	}
	qids := queryIDRe.FindAllStringSubmatchIndex(js, -1)
	if len(qids) == 0 {
		return ""
	}

	best := ""
	bestDist := int(^uint(0) >> 1)
	for mi := strings.Index(js, marker); mi >= 0; {
		for _, q := range qids {
			dist := q[0] - mi
			if dist < 0 {
				dist = -dist
			}
			if dist < bestDist {
				bestDist = dist
				best = js[q[2]:q[3]]
			}
		}
		next := strings.Index(js[mi+len(marker):], marker)
		if next < 0 {
			break
		}
		mi += len(marker) + next
	}
	return best
}

// bundleScore ranks bundle URLs by how likely they contain tweet operations.
func bundleScore(u string) int {
	score := 0
	for _, hint := range []string{"api", "endpoint", "Tweet", "tweet", "bundle"} {
		if strings.Contains(u, hint) {
			score++
		}
	}
	return score
}

func fetchBody(ctx context.Context, hc *http.Client, url, authToken, ct0 string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", loginUA)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if authToken != "" && ct0 != "" {
		req.Header.Set("Cookie", fmt.Sprintf("auth_token=%s; ct0=%s", authToken, ct0))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
