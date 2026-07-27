package api

import (
	"reflect"
	"testing"
	"time"
)

func TestSupportedBrowsers(t *testing.T) {
	want := []string{"Chrome", "Helium", "Firefox", "Brave", "Zen"}
	if got := SupportedBrowsers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedBrowsers() = %v, want %v", got, want)
	}
}

func TestBetterLoginResultPrefersXDomainThenRecency(t *testing.T) {
	now := time.Now()
	twitter := &LoginResult{CookieDomain: "twitter.com", LastUsedAt: now.Add(time.Hour)}
	xOlder := &LoginResult{CookieDomain: "x.com", LastUsedAt: now}
	if !betterLoginResult(xOlder, twitter) {
		t.Fatal("x.com session should be preferred over a twitter.com session")
	}
	xNewer := &LoginResult{CookieDomain: "x.com", LastUsedAt: now.Add(time.Minute)}
	if !betterLoginResult(xNewer, xOlder) {
		t.Fatal("newer session was not preferred within the same domain")
	}
}

func TestScanCollapsesOnlyByteIdenticalCookiePairs(t *testing.T) {
	now := time.Now()
	results := sortAndDeduplicateLoginResults([]LoginResult{
		{
			AuthToken: "same-auth", CT0: "same-ct0", Profile: "Copied",
			CookieDomain: "x.com", LastUsedAt: now.Add(-time.Hour),
		},
		{
			AuthToken: "same-auth", CT0: "same-ct0", Profile: "Fresh",
			CookieDomain: "x.com", LastUsedAt: now,
		},
		{
			AuthToken: "rotated-auth", CT0: "rotated-ct0", Profile: "Older cookies",
			CookieDomain: "x.com", LastUsedAt: now.Add(-2 * time.Hour),
		},
	})

	if len(results) != 2 {
		t.Fatalf("scan returned %d sessions, want 2 distinct cookie pairs", len(results))
	}
	if results[0].Profile != "Fresh" {
		t.Fatalf("first profile = %q, want freshest identical-cookie copy", results[0].Profile)
	}
	if results[1].Profile != "Older cookies" {
		t.Fatalf("second profile = %q, want rotated cookie pair to remain selectable", results[1].Profile)
	}
}
