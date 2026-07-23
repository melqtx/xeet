//go:build linux

package api

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/browserutils/kooky"
	"github.com/browserutils/kooky/browser/brave"
	"github.com/browserutils/kooky/browser/chrome"
	"github.com/browserutils/kooky/browser/chromium"
	"github.com/browserutils/kooky/browser/edge"
)

type chromiumCookieReader func(context.Context, string, ...kooky.Filter) ([]*kooky.Cookie, error)

type chromiumBrowser struct {
	name   string
	roots  []string
	reader chromiumCookieReader
}

func chromiumBrowsers(home string) []chromiumBrowser {
	config := filepath.Join(home, ".config")
	flatpak := filepath.Join(home, ".var", "app")
	return []chromiumBrowser{
		{"Chrome", []string{
			filepath.Join(config, "google-chrome"),
			filepath.Join(flatpak, "com.google.Chrome", "config", "google-chrome"),
		}, chrome.ReadCookies},
		{"Chrome Beta", []string{filepath.Join(config, "google-chrome-beta")}, chrome.ReadCookies},
		{"Chromium", []string{
			filepath.Join(config, "chromium"),
			filepath.Join(home, "snap", "chromium", "common", "chromium"),
			filepath.Join(flatpak, "org.chromium.Chromium", "config", "chromium"),
		}, chromium.ReadCookies},
		{"Brave", []string{
			filepath.Join(config, "BraveSoftware", "Brave-Browser"),
			filepath.Join(flatpak, "com.brave.Browser", "config", "BraveSoftware", "Brave-Browser"),
		}, brave.ReadCookies},
		{"Edge", []string{
			filepath.Join(config, "microsoft-edge"),
			filepath.Join(config, "microsoft-edge-beta"),
		}, edge.ReadCookies},
	}
}

func geckoBrowsers(home string) []geckoBrowser {
	flatpak := filepath.Join(home, ".var", "app")
	return []geckoBrowser{
		{"Firefox", []string{
			filepath.Join(home, ".mozilla", "firefox"),
			filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox"),
			filepath.Join(flatpak, "org.mozilla.firefox", ".mozilla", "firefox"),
		}},
		{"Zen", []string{
			filepath.Join(home, ".zen"),
			filepath.Join(flatpak, "app.zen_browser.zen", ".zen"),
			filepath.Join(flatpak, "io.github.zen_browser.zen", ".zen"),
		}},
	}
}

// DetectBrowsers returns installed browsers with at least one cookie database.
// Detection never opens the keyring, so browser listing cannot trigger an
// unlock prompt. Import verifies that an x.com session is actually present.
func DetectBrowsers() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var names []string
	for _, browser := range chromiumBrowsers(home) {
		if len(browser.cookieDBs()) > 0 {
			names = append(names, browser.name)
		}
	}
	for _, browser := range geckoBrowsers(home) {
		if browser.hasXSession() {
			names = append(names, browser.name)
		}
	}
	return names
}

func ImportBrowserSession(name string) (*LoginResult, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", err
	}
	if browser, ok := findGeckoBrowser(name, geckoBrowsers(home)); ok {
		return importGeckoSession(browser)
	}
	for _, browser := range chromiumBrowsers(home) {
		if browser.name == name {
			return importChromiumSession(browser)
		}
	}
	return nil, "", fmt.Errorf("unknown browser %q", name)
}

func (b chromiumBrowser) cookieDBs() []string {
	var out []string
	skip := map[string]bool{
		"Cache": true, "Code Cache": true, "GPUCache": true,
		"Service Worker": true, "IndexedDB": true, "Local Storage": true,
		"Session Storage": true, "Extensions": true, "Sessions": true,
		"Crashpad": true, "Storage": true,
	}
	for _, root := range b.roots {
		rootDepth := strings.Count(root, string(os.PathSeparator))
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != root && skip[d.Name()] {
					return fs.SkipDir
				}
				if strings.Count(path, string(os.PathSeparator))-rootDepth > 4 {
					return fs.SkipDir
				}
				return nil
			}
			if d.Name() == "Cookies" {
				out = append(out, path)
			}
			return nil
		})
	}
	return out
}

func importChromiumSession(browser chromiumBrowser) (*LoginResult, string, error) {
	dbs := browser.cookieDBs()
	if len(dbs) == 0 {
		return nil, "", fmt.Errorf("%s has no cookie database; is it installed and set up?", browser.name)
	}

	filters := []kooky.Filter{
		kooky.FilterFunc(func(cookie *kooky.Cookie) bool {
			domain := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
			return domain == "x.com" || strings.HasSuffix(domain, ".x.com") ||
				domain == "twitter.com" || strings.HasSuffix(domain, ".twitter.com")
		}),
		kooky.FilterFunc(func(cookie *kooky.Cookie) bool {
			return cookie.Name == "auth_token" || cookie.Name == "ct0"
		}),
	}

	var lastErr error
	for _, db := range dbs {
		tmp, copied, err := copyDB(db)
		if err != nil {
			lastErr = err
			continue
		}
		cookies, readErr := browser.reader(context.Background(), copied, filters...)
		os.RemoveAll(tmp)
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if result := sessionFromCookies(cookies); result != nil {
			return result, browser.name, nil
		}
	}

	if lastErr != nil {
		return nil, "", fmt.Errorf("couldn't unlock or read %s cookies: %w. Unlock your desktop keyring and try again", browser.name, lastErr)
	}
	return nil, "", fmt.Errorf("no logged-in x.com session found in %s; open x.com in it, log in, then try again", browser.name)
}
