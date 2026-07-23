package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/browserutils/kooky"
)

func TestSessionFromCookies(t *testing.T) {
	cookies := []*kooky.Cookie{
		{Cookie: http.Cookie{Name: "unrelated", Value: "ignore"}},
		{Cookie: http.Cookie{Name: "auth_token", Value: "token", Domain: ".x.com"}},
		{Cookie: http.Cookie{Name: "ct0", Value: "csrf", Domain: ".x.com"}},
	}
	got := sessionFromCookies(cookies)
	if got == nil || got.AuthToken != "token" || got.CT0 != "csrf" || got.CookieDomain != "x.com" {
		t.Fatalf("sessionFromCookies() = %+v", got)
	}
}

func TestSessionFromCookiesRequiresBothValues(t *testing.T) {
	if got := sessionFromCookies([]*kooky.Cookie{{
		Cookie: http.Cookie{Name: "auth_token", Value: "token", Domain: ".x.com"},
	}}); got != nil {
		t.Fatalf("got partial session %+v", got)
	}
}

func TestSessionFromCookiesKeepsPairOnSameDomain(t *testing.T) {
	now := time.Now()
	cookies := []*kooky.Cookie{
		{Cookie: http.Cookie{Name: "auth_token", Value: "x-auth", Domain: ".x.com"}, Creation: now},
		{Cookie: http.Cookie{Name: "ct0", Value: "twitter-ct0", Domain: ".twitter.com"}, Creation: now.Add(time.Minute)},
		{Cookie: http.Cookie{Name: "ct0", Value: "x-ct0", Domain: ".x.com"}, Creation: now.Add(-time.Minute)},
		{Cookie: http.Cookie{Name: "auth_token", Value: "twitter-auth", Domain: ".twitter.com"}, Creation: now.Add(2 * time.Minute)},
	}
	got := sessionFromCookies(cookies)
	if got == nil || got.AuthToken != "x-auth" || got.CT0 != "x-ct0" || got.CookieDomain != "x.com" {
		t.Fatalf("mixed or wrong-domain session selected: %+v", got)
	}
}

func TestSessionFromCookiesUsesNewestUnexpiredPair(t *testing.T) {
	now := time.Now()
	cookies := []*kooky.Cookie{
		{Cookie: http.Cookie{Name: "auth_token", Value: "old-auth", Domain: ".x.com"}, Creation: now.Add(-time.Hour)},
		{Cookie: http.Cookie{Name: "ct0", Value: "old-ct0", Domain: ".x.com"}, Creation: now.Add(-time.Hour)},
		{Cookie: http.Cookie{Name: "auth_token", Value: "new-auth", Domain: ".x.com", Expires: now.Add(time.Hour)}, Creation: now},
		{Cookie: http.Cookie{Name: "ct0", Value: "new-ct0", Domain: ".x.com", Expires: now.Add(time.Hour)}, Creation: now},
		{Cookie: http.Cookie{Name: "auth_token", Value: "expired-auth", Domain: ".x.com", Expires: now.Add(-time.Minute)}, Creation: now.Add(time.Minute)},
	}
	got := sessionFromCookies(cookies)
	if got == nil || got.AuthToken != "new-auth" || got.CT0 != "new-ct0" {
		t.Fatalf("newest valid pair not selected: %+v", got)
	}
}

func TestGeckoBrowserFindsMultipleProfilesAndRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{filepath.Join(dir, "native"), filepath.Join(dir, "flatpak")}
	for i, root := range roots {
		profile := filepath.Join(root, "Profiles", string(rune('a'+i))+".default")
		if err := os.MkdirAll(profile, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(profile, "cookies.sqlite"), []byte("db"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	browser := geckoBrowser{name: "Firefox", roots: roots}
	if got := len(browser.cookieDBs()); got != 2 {
		t.Fatalf("found %d databases, want 2", got)
	}
}

func TestCopyDBAsCopiesWAL(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "cookies.sqlite")
	if err := os.WriteFile(source, []byte("main"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source+"-wal", []byte("wal"), 0600); err != nil {
		t.Fatal(err)
	}

	tmp, copied, err := copyDBAs(source, "cookies.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	for path, want := range map[string]string{copied: "main", copied + "-wal": "wal"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want %q", path, data, want)
		}
	}
}
