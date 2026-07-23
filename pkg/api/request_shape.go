package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// RequestShape contains only structural metadata from a CreateTweet request.
// It intentionally excludes every header value, cookie value, and body value.
type RequestShape struct {
	HeaderNames     []string
	CookieNames     []string
	BodyKeys        []string
	VariableKeys    []string
	FeatureKeys     []string
	UserAgentFamily string
	UserAgentMajor  string
	Platform        string
}

// RequestShapeComparison compares a successful browser request with the
// request Xeet currently builds. Every field is safe to print.
type RequestShapeComparison struct {
	Browser RequestShape
	Xeet    RequestShape
}

func (c *RequestShapeComparison) String() string {
	var out strings.Builder
	fmt.Fprintf(&out, "browser: %s on %s, %d headers, %d cookies, %d features\n",
		displayBrowser(c.Browser), displayValue(c.Browser.Platform),
		len(c.Browser.HeaderNames), len(c.Browser.CookieNames), len(c.Browser.FeatureKeys))
	fmt.Fprintf(&out, "xeet: %s on %s, %d headers, %d cookies, %d features\n",
		displayBrowser(c.Xeet), displayValue(c.Xeet.Platform),
		len(c.Xeet.HeaderNames), len(c.Xeet.CookieNames), len(c.Xeet.FeatureKeys))
	fmt.Fprintf(&out, "browser-only headers: %s\n", displayList(difference(c.Browser.HeaderNames, c.Xeet.HeaderNames)))
	fmt.Fprintf(&out, "xeet-only headers: %s\n", displayList(difference(c.Xeet.HeaderNames, c.Browser.HeaderNames)))
	fmt.Fprintf(&out, "browser-only cookies: %s\n", displayList(difference(c.Browser.CookieNames, c.Xeet.CookieNames)))
	fmt.Fprintf(&out, "xeet-only cookies: %s\n", displayList(difference(c.Xeet.CookieNames, c.Browser.CookieNames)))
	fmt.Fprintf(&out, "browser-only body keys: %s\n", displayList(difference(c.Browser.BodyKeys, c.Xeet.BodyKeys)))
	fmt.Fprintf(&out, "xeet-only body keys: %s\n", displayList(difference(c.Xeet.BodyKeys, c.Browser.BodyKeys)))
	fmt.Fprintf(&out, "browser-only variable keys: %s\n", displayList(difference(c.Browser.VariableKeys, c.Xeet.VariableKeys)))
	fmt.Fprintf(&out, "xeet-only variable keys: %s\n", displayList(difference(c.Xeet.VariableKeys, c.Browser.VariableKeys)))
	fmt.Fprintf(&out, "browser-only features: %s\n", displayList(difference(c.Browser.FeatureKeys, c.Xeet.FeatureKeys)))
	fmt.Fprintf(&out, "xeet-only features: %s\n", displayList(difference(c.Xeet.FeatureKeys, c.Browser.FeatureKeys)))
	return out.String()
}

func displayBrowser(shape RequestShape) string {
	family := displayValue(shape.UserAgentFamily)
	if shape.UserAgentMajor == "" {
		return family
	}
	return family + " " + shape.UserAgentMajor
}

func displayValue(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func displayList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

type harDocument struct {
	Log struct {
		Entries []harEntry `json:"entries"`
	} `json:"log"`
}

type harEntry struct {
	Request harRequest `json:"request"`
}

type harRequest struct {
	Method   string         `json:"method"`
	URL      string         `json:"url"`
	Headers  []harNameValue `json:"headers"`
	Cookies  []harNameValue `json:"cookies"`
	PostData harPostData    `json:"postData"`
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harPostData struct {
	Text string `json:"text"`
}

// CompareCreateTweetHAR finds the last CreateTweet POST in a browser HAR and
// compares its safe structural shape with Xeet's current request.
func CompareCreateTweetHAR(reader io.Reader) (*RequestShapeComparison, error) {
	decoder := json.NewDecoder(reader)
	var document harDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode HAR: %w", err)
	}

	var matched *harRequest
	for i := range document.Log.Entries {
		request := &document.Log.Entries[i].Request
		if strings.EqualFold(request.Method, http.MethodPost) && isCreateTweetURL(request.URL) {
			matched = request
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("HAR contains no CreateTweet POST request")
	}

	browserShape, err := requestShapeFromHAR(matched)
	if err != nil {
		return nil, err
	}
	xeetShape, err := currentCreateTweetRequestShape()
	if err != nil {
		return nil, err
	}
	return &RequestShapeComparison{Browser: browserShape, Xeet: xeetShape}, nil
}

func isCreateTweetURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Hostname() == "x.com" && strings.HasSuffix(parsed.Path, "/CreateTweet")
}

func requestShapeFromHAR(request *harRequest) (RequestShape, error) {
	headers := make([]string, 0, len(request.Headers))
	cookies := make([]string, 0, len(request.Cookies)+2)
	userAgent := ""
	for _, header := range request.Headers {
		name := normalizeMetadataName(header.Name)
		if name == "" {
			continue
		}
		headers = append(headers, name)
		switch name {
		case "cookie":
			cookies = append(cookies, cookieNames(header.Value)...)
		case "user-agent":
			userAgent = header.Value
		}
	}
	for _, cookie := range request.Cookies {
		if name := normalizeMetadataName(cookie.Name); name != "" {
			cookies = append(cookies, name)
		}
	}

	bodyKeys, variableKeys, featureKeys, err := requestBodyShape([]byte(request.PostData.Text))
	if err != nil {
		return RequestShape{}, fmt.Errorf("matched CreateTweet request has invalid JSON body")
	}
	family, major, platform := summarizeUserAgent(userAgent)
	return RequestShape{
		HeaderNames: headers, CookieNames: cookies,
		BodyKeys: bodyKeys, VariableKeys: variableKeys, FeatureKeys: featureKeys,
		UserAgentFamily: family, UserAgentMajor: major, Platform: platform,
	}.normalized(), nil
}

func currentCreateTweetRequestShape() (RequestShape, error) {
	client := &WebClient{authToken: "redacted", ct0: "redacted"}
	request, err := http.NewRequest(http.MethodPost, "https://x.com/i/api/graphql/redacted/CreateTweet", bytes.NewReader(nil))
	if err != nil {
		return RequestShape{}, err
	}
	client.setCreateTweetBrowserHeaders(request)
	request.Header.Set("X-Client-Transaction-Id", "generated-per-request")

	headerNames := make([]string, 0, len(request.Header))
	for name := range request.Header {
		headerNames = append(headerNames, normalizeMetadataName(name))
	}
	body, err := json.Marshal(map[string]any{
		"variables": newCreateTweetVariables("redacted"),
		"features":  createTweetFeatures,
		"queryId":   "redacted",
	})
	if err != nil {
		return RequestShape{}, err
	}
	bodyKeys, variableKeys, featureKeys, err := requestBodyShape(body)
	if err != nil {
		return RequestShape{}, err
	}
	family, major, platform := summarizeUserAgent(request.Header.Get("User-Agent"))
	return RequestShape{
		HeaderNames: headerNames, CookieNames: cookieNames(request.Header.Get("Cookie")),
		BodyKeys: bodyKeys, VariableKeys: variableKeys, FeatureKeys: featureKeys,
		UserAgentFamily: family, UserAgentMajor: major, Platform: platform,
	}.normalized(), nil
}

func requestBodyShape(body []byte) ([]string, []string, []string, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, nil, err
	}
	bodyKeys := mapKeys(payload)

	var variables map[string]json.RawMessage
	if raw := payload["variables"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &variables); err != nil {
			return nil, nil, nil, err
		}
	}
	var features map[string]json.RawMessage
	if raw := payload["features"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &features); err != nil {
			return nil, nil, nil, err
		}
	}
	return bodyKeys, mapKeys(variables), mapKeys(features), nil
}

func mapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if safe := normalizeMetadataName(key); safe != "" {
			keys = append(keys, safe)
		}
	}
	return uniqueSorted(keys)
}

func cookieNames(header string) []string {
	var names []string
	for _, part := range strings.Split(header, ";") {
		name, _, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		if safe := normalizeMetadataName(name); safe != "" {
			names = append(names, safe)
		}
	}
	return uniqueSorted(names)
}

func normalizeMetadataName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') ||
			strings.ContainsRune("-_.:", char) {
			continue
		}
		return ""
	}
	return value
}

func summarizeUserAgent(userAgent string) (string, string, string) {
	lower := strings.ToLower(userAgent)
	family := "other"
	versionToken := ""
	switch {
	case strings.Contains(lower, "firefox/"):
		family = "firefox"
		versionToken = "firefox/"
	case strings.Contains(lower, "chrome/") || strings.Contains(lower, "chromium/"):
		family = "chromium"
		versionToken = "chrome/"
		if strings.Contains(lower, "chromium/") {
			versionToken = "chromium/"
		}
	case strings.Contains(lower, "safari/"):
		family = "safari"
		versionToken = "version/"
	}
	major := userAgentMajor(lower, versionToken)
	platform := "other"
	switch {
	case strings.Contains(lower, "macintosh") || strings.Contains(lower, "mac os"):
		platform = "macos"
	case strings.Contains(lower, "windows"):
		platform = "windows"
	case strings.Contains(lower, "linux"):
		platform = "linux"
	}
	return family, major, platform
}

func userAgentMajor(userAgent, token string) string {
	if token == "" {
		return ""
	}
	_, version, found := strings.Cut(userAgent, token)
	if !found {
		return ""
	}
	major, _, _ := strings.Cut(version, ".")
	for _, char := range major {
		if char < '0' || char > '9' {
			return ""
		}
	}
	return major
}

func (shape RequestShape) normalized() RequestShape {
	shape.HeaderNames = uniqueSorted(shape.HeaderNames)
	shape.CookieNames = uniqueSorted(shape.CookieNames)
	shape.BodyKeys = uniqueSorted(shape.BodyKeys)
	shape.VariableKeys = uniqueSorted(shape.VariableKeys)
	shape.FeatureKeys = uniqueSorted(shape.FeatureKeys)
	return shape
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func difference(left, right []string) []string {
	rightSet := make(map[string]bool, len(right))
	for _, value := range right {
		rightSet[value] = true
	}
	var result []string
	for _, value := range left {
		if !rightSet[value] {
			result = append(result, value)
		}
	}
	return result
}
