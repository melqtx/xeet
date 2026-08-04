//go:build linux

package api

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/browserutils/kooky"
	"github.com/browserutils/kooky/browser/brave"
	"github.com/browserutils/kooky/browser/chrome"
	"github.com/browserutils/kooky/browser/chromium"
	"github.com/melqtx/xeet/internal/wsl"
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
		{"Helium", []string{
			filepath.Join(config, "net.imput.helium"),
		}, chromium.ReadCookies},
		{"Brave", []string{
			filepath.Join(config, "BraveSoftware", "Brave-Browser"),
			filepath.Join(flatpak, "com.brave.Browser", "config", "BraveSoftware", "Brave-Browser"),
		}, brave.ReadCookies},
	}
}

func geckoBrowsers(home string) []geckoBrowser {
	browsers, _ := geckoBrowsersForEnvironment(home)
	return browsers
}

func geckoBrowsersForEnvironment(home string) ([]geckoBrowser, error) {
	var windowsAppData string
	var interopErr error
	if wsl.Active() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		windowsAppData, interopErr = wsl.AppData(ctx)
	}
	return geckoBrowsersAt(home, windowsAppData), interopErr
}

func geckoBrowsersAt(home, windowsAppData string) []geckoBrowser {
	flatpak := filepath.Join(home, ".var", "app")
	firefoxRoots := []string{
		filepath.Join(home, ".mozilla", "firefox"),
		filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox"),
		filepath.Join(flatpak, "org.mozilla.firefox", ".mozilla", "firefox"),
	}
	var windowsFirefoxRoot string
	if windowsAppData != "" {
		windowsFirefoxRoot = filepath.Join(windowsAppData, "Mozilla", "Firefox")
		firefoxRoots = append(firefoxRoots, windowsFirefoxRoot)
	}
	return []geckoBrowser{
		{name: "Firefox", roots: firefoxRoots, windowsRoot: windowsFirefoxRoot},
		{name: "Zen", roots: []string{
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
	chromium := chromiumBrowsers(home)
	gecko := geckoBrowsers(home)
	var names []string
	for _, name := range SupportedBrowsers() {
		if browser, ok := findChromiumBrowser(name, chromium); ok && len(browser.cookieDBs()) > 0 {
			names = append(names, name)
			continue
		}
		if browser, ok := findGeckoBrowser(name, gecko); ok && browser.hasXSession() {
			names = append(names, name)
		}
	}
	return names
}

func ImportBrowserSession(name string) (*LoginResult, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", err
	}
	gecko, interopErr := geckoBrowsersForEnvironment(home)
	if browser, ok := findGeckoBrowser(name, gecko); ok {
		result, resolved, err := importGeckoSession(browser)
		if err != nil && interopErr != nil {
			return nil, "", fmt.Errorf("%w; WSL detected but Windows Firefox could not be checked: %v", err, interopErr)
		}
		return result, resolved, err
	}
	if browser, ok := findChromiumBrowser(name, chromiumBrowsers(home)); ok {
		return importChromiumSession(browser)
	}
	return nil, "", fmt.Errorf("unknown browser %q", name)
}

func findChromiumBrowser(name string, browsers []chromiumBrowser) (chromiumBrowser, bool) {
	for _, browser := range browsers {
		if browser.name == name {
			return browser, true
		}
	}
	return chromiumBrowser{}, false
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
	sort.Strings(out)
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
	var best *LoginResult
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
			result.Profile = cookieProfile(db)
			if result.LastUsedAt.IsZero() {
				if info, statErr := os.Stat(db); statErr == nil {
					result.LastUsedAt = info.ModTime()
				}
			}
			if betterLoginResult(result, best) {
				best = result
			}
		}
	}
	if best != nil {
		return best, browser.name, nil
	}

	if lastErr != nil {
		return nil, "", fmt.Errorf("couldn't unlock or read %s cookies: %w. Unlock your desktop keyring and try again", browser.name, lastErr)
	}
	return nil, "", fmt.Errorf("no logged-in x.com session found in %s; open x.com in it, log in, then try again", browser.name)
}
