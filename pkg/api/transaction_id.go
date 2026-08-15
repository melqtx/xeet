package api

// Transaction-id generation is a Go adaptation of the algorithm documented by
// iSarabjitDhiman/XClientTransaction (MIT License). It derives the per-request
// X-Client-Transaction-Id from X's current home page and ondemand.s bundle
// instead of replaying a stale value captured from a browser.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const (
	transactionEpoch   = int64(1682924400)
	transactionKeyword = "obfiowerehiring"
)

var (
	ondemandIndexRE    = regexp.MustCompile(`,(\d+):["']ondemand\.s["']`)
	transactionIndexRE = regexp.MustCompile(`\([A-Za-z_$]\[(\d{1,2})\],\s*16\)`)
	digitsRE           = regexp.MustCompile(`\d+`)
)

type transactionIDGenerator struct {
	mu           sync.Mutex
	keyBytes     []byte
	animationKey string
	loadedAt     time.Time
}

// Transaction signing state is reusable across short-lived clients, but the
// home page that supplies it is authenticated. Keep one cache per session so a
// reauthentication cannot reuse another account's state.
var transactionIDCaches sync.Map

func transactionIDsForSession(authToken, ct0 string) *transactionIDGenerator {
	key := sha256.Sum256([]byte(authToken + "\x00" + ct0))
	cached, _ := transactionIDCaches.LoadOrStore(key, &transactionIDGenerator{})
	return cached.(*transactionIDGenerator)
}

func (c *WebClient) generateTransactionID(ctx context.Context, method, path string) (string, error) {
	if c.transactionIDs == nil {
		c.transactionIDs = &transactionIDGenerator{}
	}
	return c.transactionIDs.generate(ctx, c, method, path, time.Now())
}

func (g *transactionIDGenerator) generate(ctx context.Context, client *WebClient, method, path string, now time.Time) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.keyBytes) == 0 || g.animationKey == "" || time.Since(g.loadedAt) > 15*time.Minute {
		key, animation, err := loadTransactionState(ctx, client)
		if err != nil {
			return "", err
		}
		g.keyBytes = key
		g.animationKey = animation
		g.loadedAt = time.Now()
	}
	return encodeTransactionID(method, path, now, g.keyBytes, g.animationKey)
}

func loadTransactionState(ctx context.Context, client *WebClient) ([]byte, string, error) {
	home, err := fetchTransactionResource(ctx, client, "https://x.com/home", true)
	if err != nil {
		return nil, "", fmt.Errorf("load X transaction page: %w", err)
	}
	keyText, frames, err := parseTransactionHome(home)
	if err != nil {
		return nil, "", err
	}
	keyBytes, err := base64.StdEncoding.DecodeString(keyText)
	if err != nil || len(keyBytes) < 6 {
		return nil, "", fmt.Errorf("decode X transaction key")
	}

	chunkMatch := ondemandIndexRE.FindSubmatch(home)
	if len(chunkMatch) != 2 {
		return nil, "", fmt.Errorf("find X ondemand bundle index")
	}
	hashRE := regexp.MustCompile(`,` + regexp.QuoteMeta(string(chunkMatch[1])) + `:"([0-9a-f]+)"`)
	hashMatch := hashRE.FindSubmatch(home)
	if len(hashMatch) != 2 {
		return nil, "", fmt.Errorf("find X ondemand bundle hash")
	}
	bundleURL := "https://abs.twimg.com/responsive-web/client-web/ondemand.s." + string(hashMatch[1]) + "a.js"
	bundle, err := fetchTransactionResource(ctx, client, bundleURL, false)
	if err != nil {
		return nil, "", fmt.Errorf("load X ondemand bundle: %w", err)
	}
	matches := transactionIndexRE.FindAllSubmatch(bundle, -1)
	if len(matches) < 2 {
		return nil, "", fmt.Errorf("parse X transaction indices")
	}
	indices := make([]int, 0, len(matches))
	for _, match := range matches {
		index, parseErr := strconv.Atoi(string(match[1]))
		if parseErr != nil || index >= len(keyBytes) {
			return nil, "", fmt.Errorf("invalid X transaction index")
		}
		indices = append(indices, index)
	}
	animation, err := buildAnimationKey(keyBytes, indices[0], indices[1:], frames)
	if err != nil {
		return nil, "", err
	}
	return keyBytes, animation, nil
}

func fetchTransactionResource(ctx context.Context, client *WebClient, target string, authenticated bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	if authenticated {
		// The HTML navigation request is not an API call: sending the bearer and
		// JSON headers here makes x.com answer 401 even with valid cookies.
		req.Header.Set("Cookie", fmt.Sprintf("auth_token=%s; ct0=%s", client.authToken, client.ct0))
		req.Header.Set("User-Agent", client.browserUserAgent())
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Referer", "https://x.com/")
	} else {
		req.Header.Set("User-Agent", client.browserUserAgent())
	}
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return body, nil
}

func parseTransactionHome(body []byte) (string, [4]string, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return "", [4]string{}, fmt.Errorf("parse X transaction page: %w", err)
	}
	var key string
	var frames [4]string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "meta" && attr(node, "name") == "twitter-site-verification" {
			key = attr(node, "content")
		}
		if node.Type == html.ElementNode && node.Data == "svg" {
			id := attr(node, "id")
			if strings.HasPrefix(id, "loading-x-anim-") {
				index, parseErr := strconv.Atoi(strings.TrimPrefix(id, "loading-x-anim-"))
				if parseErr == nil && index >= 0 && index < len(frames) {
					var paths []string
					collectPathData(node, &paths)
					if len(paths) > 1 {
						frames[index] = paths[1]
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if key == "" {
		return "", frames, fmt.Errorf("find X transaction verification key")
	}
	for _, frame := range frames {
		if frame == "" {
			return "", frames, fmt.Errorf("find X transaction animation frames")
		}
	}
	return key, frames, nil
}

func attr(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

func collectPathData(node *html.Node, paths *[]string) {
	if node.Type == html.ElementNode && node.Data == "path" {
		*paths = append(*paths, attr(node, "d"))
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectPathData(child, paths)
	}
}

func buildAnimationKey(key []byte, rowIndex int, timeIndices []int, frames [4]string) (string, error) {
	if rowIndex >= len(key) {
		return "", fmt.Errorf("x transaction row index out of range")
	}
	rows, err := parseFrameRows(frames[int(key[5])%len(frames)])
	if err != nil {
		return "", err
	}
	row := int(key[rowIndex]) % 16
	if row >= len(rows) || len(rows[row]) < 11 {
		return "", fmt.Errorf("x transaction animation row unavailable")
	}
	frameTime := 1
	for _, index := range timeIndices {
		if index >= len(key) {
			return "", fmt.Errorf("x transaction time index out of range")
		}
		frameTime *= int(key[index]) % 16
	}
	frameTime = int(jsRound(float64(frameTime)/10.0)) * 10
	return animateTransaction(rows[row], float64(frameTime)/4096.0), nil
}

func parseFrameRows(path string) ([][]int, error) {
	if len(path) <= 9 {
		return nil, fmt.Errorf("invalid X transaction animation path")
	}
	parts := strings.Split(path[9:], "C")
	rows := make([][]int, 0, len(parts))
	for _, part := range parts {
		matches := digitsRE.FindAllString(part, -1)
		row := make([]int, 0, len(matches))
		for _, match := range matches {
			value, _ := strconv.Atoi(match)
			row = append(row, value)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func animateTransaction(frame []int, targetTime float64) string {
	fromColor := []float64{float64(frame[0]), float64(frame[1]), float64(frame[2]), 1}
	toColor := []float64{float64(frame[3]), float64(frame[4]), float64(frame[5]), 1}
	toRotation := solveTransaction(float64(frame[6]), 60, 360, true)
	curve := make([]float64, 0, 4)
	for index, value := range frame[7:] {
		minValue := 0.0
		if index%2 == 1 {
			minValue = -1
		}
		curve = append(curve, solveTransaction(float64(value), minValue, 1, false))
	}
	value := cubicValue(curve, targetTime)
	colors := interpolateTransaction(fromColor, toColor, value)
	rotation := interpolateTransaction([]float64{0}, []float64{toRotation}, value)[0]
	radians := rotation * math.Pi / 180
	matrix := []float64{math.Cos(radians), -math.Sin(radians), math.Sin(radians), math.Cos(radians)}

	var output strings.Builder
	for _, color := range colors[:3] {
		color = math.Max(0, math.Min(255, color))
		output.WriteString(strconv.FormatInt(int64(math.RoundToEven(color)), 16))
	}
	for _, item := range matrix {
		item = math.Abs(math.Round(item*100) / 100)
		hex := transactionFloatHex(item)
		if strings.HasPrefix(hex, ".") {
			hex = "0" + hex
		}
		if hex == "" {
			hex = "0"
		}
		output.WriteString(strings.ToLower(hex))
	}
	output.WriteString("00")
	return strings.NewReplacer(".", "", "-", "").Replace(output.String())
}

func solveTransaction(value, minValue, maxValue float64, floor bool) float64 {
	result := value*(maxValue-minValue)/255 + minValue
	if floor {
		return math.Floor(result)
	}
	return math.Round(result*100) / 100
}

func cubicValue(curve []float64, value float64) float64 {
	if len(curve) < 4 {
		return 0
	}
	if value <= 0 {
		gradient := 0.0
		if curve[0] > 0 {
			gradient = curve[1] / curve[0]
		} else if curve[1] == 0 && curve[2] > 0 {
			gradient = curve[3] / curve[2]
		}
		return gradient * value
	}
	if value >= 1 {
		gradient := 0.0
		if curve[2] < 1 {
			gradient = (curve[3] - 1) / (curve[2] - 1)
		} else if curve[2] == 1 && curve[0] < 1 {
			gradient = (curve[1] - 1) / (curve[0] - 1)
		}
		return 1 + gradient*(value-1)
	}
	start, end, middle := 0.0, 1.0, 0.5
	for iteration := 0; iteration < 100; iteration++ {
		middle = (start + end) / 2
		estimate := cubicCalculate(curve[0], curve[2], middle)
		if math.Abs(value-estimate) < 0.00001 {
			break
		}
		if estimate < value {
			start = middle
		} else {
			end = middle
		}
	}
	return cubicCalculate(curve[1], curve[3], middle)
}

func cubicCalculate(a, b, middle float64) float64 {
	return 3*a*(1-middle)*(1-middle)*middle + 3*b*(1-middle)*middle*middle + middle*middle*middle
}

func interpolateTransaction(from, to []float64, value float64) []float64 {
	result := make([]float64, len(from))
	for index := range from {
		result[index] = from[index]*(1-value) + to[index]*value
	}
	return result
}

func transactionFloatHex(value float64) string {
	whole := int64(value)
	fraction := value - float64(whole)
	result := ""
	if whole > 0 {
		result = strconv.FormatInt(whole, 16)
	}
	if fraction == 0 {
		return result
	}
	result += "."
	for count := 0; fraction > 0 && count < 64; count++ {
		fraction *= 16
		digit := int(fraction)
		fraction -= float64(digit)
		result += strconv.FormatInt(int64(digit), 16)
	}
	return result
}

func jsRound(value float64) float64 {
	floor := math.Floor(value)
	if value-floor >= 0.5 {
		return math.Ceil(value)
	}
	return math.Copysign(floor, value)
}

func encodeTransactionID(method, path string, now time.Time, key []byte, animationKey string) (string, error) {
	seconds := now.Unix() - transactionEpoch
	timeBytes := []byte{byte(seconds), byte(seconds >> 8), byte(seconds >> 16), byte(seconds >> 24)}
	digest := sha256.Sum256([]byte(strings.ToUpper(method) + "!" + path + "!" + strconv.FormatInt(seconds, 10) + transactionKeyword + animationKey))
	plain := make([]byte, 0, len(key)+len(timeBytes)+16+1)
	plain = append(plain, key...)
	plain = append(plain, timeBytes...)
	plain = append(plain, digest[:16]...)
	plain = append(plain, 3)
	var mask [1]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return "", err
	}
	encoded := make([]byte, len(plain)+1)
	encoded[0] = mask[0]
	for index, value := range plain {
		encoded[index+1] = value ^ mask[0]
	}
	return base64.RawStdEncoding.EncodeToString(encoded), nil
}
