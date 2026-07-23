package api

import (
	"strings"
	"testing"
)

func TestCompareCreateTweetHARNeverPrintsValues(t *testing.T) {
	const (
		authSecret = "auth-super-secret"
		ct0Secret  = "csrf-super-secret"
		draft      = "private draft words"
		bearer     = "bearer-super-secret"
	)
	har := `{
		"log": {
			"entries": [{
				"request": {
					"method": "POST",
					"url": "https://x.com/i/api/graphql/browser-qid/CreateTweet",
					"headers": [
						{"name": "Authorization", "value": "` + bearer + `"},
						{"name": "Cookie", "value": "auth_token=` + authSecret + `; ct0=` + ct0Secret + `; guest_id=guest-secret"},
						{"name": "User-Agent", "value": "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"},
						{"name": "X-Client-Transaction-Id", "value": "transaction-secret"}
					],
					"cookies": [
						{"name": "auth_token", "value": "` + authSecret + `"},
						{"name": "ct0", "value": "` + ct0Secret + `"}
					],
					"postData": {
						"text": "{\"variables\":{\"tweet_text\":\"` + draft + `\",\"dark_request\":false},\"features\":{\"browser_feature\":true},\"queryId\":\"browser-qid\"}"
					}
				},
				"response": {
					"content": {"text": "{\"echo\":\"` + draft + `\"}"}
				}
			}]
		}
	}`

	comparison, err := CompareCreateTweetHAR(strings.NewReader(har))
	if err != nil {
		t.Fatal(err)
	}
	output := comparison.String()
	for _, secret := range []string{authSecret, ct0Secret, draft, bearer, "transaction-secret", "browser-qid"} {
		if strings.Contains(output, secret) {
			t.Fatalf("comparison leaked %q:\n%s", secret, output)
		}
	}
	if !containsString(comparison.Browser.HeaderNames, "x-client-transaction-id") ||
		!containsString(comparison.Xeet.HeaderNames, "x-client-transaction-id") {
		t.Fatalf("transaction header missing from compared shapes: %+v", comparison)
	}
	for _, expected := range []string{
		"browser: firefox 128 on linux",
		"guest_id",
		"browser_feature",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("comparison missing %q:\n%s", expected, output)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestCompareCreateTweetHARUsesLastMatchingRequest(t *testing.T) {
	har := `{"log":{"entries":[
		{"request":{"method":"GET","url":"https://x.com/i/api/graphql/one/CreateTweet"}},
		{"request":{"method":"POST","url":"https://example.com/i/api/graphql/two/CreateTweet"}},
		{"request":{"method":"POST","url":"https://x.com/i/api/graphql/three/CreateTweet",
			"headers":[{"name":"User-Agent","value":"Mozilla/5.0 (Macintosh) Chrome/126.0"}],
			"postData":{"text":"{\"variables\":{\"tweet_text\":\"first\"},\"features\":{\"old_feature\":true},\"queryId\":\"three\"}"}}},
		{"request":{"method":"POST","url":"https://x.com/i/api/graphql/four/CreateTweet",
			"headers":[{"name":"User-Agent","value":"Mozilla/5.0 (Windows NT 10.0) Chrome/126.0"}],
			"postData":{"text":"{\"variables\":{\"tweet_text\":\"last\"},\"features\":{\"new_feature\":true},\"queryId\":\"four\"}"}}}
	]}}`
	comparison, err := CompareCreateTweetHAR(strings.NewReader(har))
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Browser.Platform != "windows" ||
		!strings.Contains(comparison.String(), "new_feature") ||
		strings.Contains(comparison.String(), "old_feature") {
		t.Fatalf("wrong request selected:\n%s", comparison.String())
	}
}

func TestCompareCreateTweetHARRejectsMissingOrMalformedRequest(t *testing.T) {
	for _, har := range []string{
		`{"log":{"entries":[]}}`,
		`{"log":{"entries":[{"request":{"method":"POST","url":"https://x.com/i/api/graphql/qid/CreateTweet","postData":{"text":"not-json"}}}]}}`,
	} {
		if _, err := CompareCreateTweetHAR(strings.NewReader(har)); err == nil {
			t.Fatalf("HAR %s was accepted", har)
		}
	}
}
