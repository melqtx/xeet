//go:build linux

package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/browserutils/kooky"
)

func TestLinuxChromiumProfiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "google-chrome")
	for _, profile := range []string{"Default", "Profile 2"} {
		path := filepath.Join(root, profile, "Network")
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "Cookies"), []byte("db"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	browser := chromiumBrowser{name: "Chrome", roots: []string{root}}
	if got := len(browser.cookieDBs()); got != 2 {
		t.Fatalf("found %d databases, want 2", got)
	}
}

func TestLinuxChromiumImportUsesCopiedDatabase(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chromium", "Default")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, "Cookies")
	if err := os.WriteFile(original, []byte("db"), 0600); err != nil {
		t.Fatal(err)
	}

	var readPath string
	browser := chromiumBrowser{
		name:  "Chromium",
		roots: []string{filepath.Dir(root)},
		reader: func(ctx context.Context, path string, filters ...kooky.Filter) ([]*kooky.Cookie, error) {
			readPath = path
			return []*kooky.Cookie{
				{Cookie: http.Cookie{Name: "auth_token", Value: "token", Domain: ".x.com"}},
				{Cookie: http.Cookie{Name: "ct0", Value: "csrf", Domain: ".x.com"}},
			}, nil
		},
	}
	result, name, err := importChromiumSession(browser)
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthToken != "token" || result.CT0 != "csrf" || name != "Chromium" {
		t.Fatalf("result=%+v name=%q", result, name)
	}
	if readPath == original || filepath.Base(readPath) != "Cookies" {
		t.Fatalf("reader path = %q, expected a copied Cookies DB", readPath)
	}
	if _, err := os.Stat(filepath.Dir(readPath)); !os.IsNotExist(err) {
		t.Fatalf("temporary cookie directory was not removed: %v", err)
	}
}

func TestLinuxLockedKeyringErrorIsActionable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chrome", "Default")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Cookies"), []byte("db"), 0600); err != nil {
		t.Fatal(err)
	}
	browser := chromiumBrowser{
		name:  "Chrome",
		roots: []string{filepath.Dir(root)},
		reader: func(context.Context, string, ...kooky.Filter) ([]*kooky.Cookie, error) {
			return nil, errors.New("collection is locked")
		},
	}
	_, _, err := importChromiumSession(browser)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unlock") {
		t.Fatalf("error = %v, want unlock guidance", err)
	}
}
