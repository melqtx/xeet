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
