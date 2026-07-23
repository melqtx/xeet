package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/browserutils/kooky"
)

func TestSessionFromCookies(t *testing.T) {
	cookies := []*kooky.Cookie{
		{Cookie: http.Cookie{Name: "unrelated", Value: "ignore"}},
		{Cookie: http.Cookie{Name: "auth_token", Value: "token"}},
		{Cookie: http.Cookie{Name: "ct0", Value: "csrf"}},
	}
	got := sessionFromCookies(cookies)
	if got == nil || got.AuthToken != "token" || got.CT0 != "csrf" {
		t.Fatalf("sessionFromCookies() = %+v", got)
	}
}

func TestSessionFromCookiesRequiresBothValues(t *testing.T) {
	if got := sessionFromCookies([]*kooky.Cookie{{Cookie: http.Cookie{Name: "auth_token", Value: "token"}}}); got != nil {
		t.Fatalf("got partial session %+v", got)
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
