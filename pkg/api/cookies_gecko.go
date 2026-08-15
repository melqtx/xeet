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
	"github.com/browserutils/kooky/browser/firefox"
)

// geckoBrowser describes Firefox and Firefox-derived browsers. Their cookies
// are stored in plain SQLite files, so no browser keyring access is needed.
type geckoBrowser struct {
	name        string
	roots       []string
	windowsRoot string
	reader      func(string) ([]*kooky.Cookie, error)
}

func (b geckoBrowser) cookieDBs() []string {
	var out []string
	for _, root := range b.roots {
		rootDepth := strings.Count(root, string(os.PathSeparator))
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != root && strings.Count(path, string(os.PathSeparator))-rootDepth > 3 {
					return fs.SkipDir
				}
				return nil
			}
			if d.Name() == "cookies.sqlite" {
				out = append(out, path)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func (b geckoBrowser) hasXSession() bool {
	for _, db := range b.cookieDBs() {
		tmp, copied, err := copyDBAs(db, "cookies.sqlite")
		if err != nil {
			continue
		}
		cookies, err := readGeckoXCookies(copied)
		os.RemoveAll(tmp)
		if err == nil && sessionFromCookies(cookies) != nil {
			return true
		}
	}
	return false
}

func readGeckoXCookies(path string) ([]*kooky.Cookie, error) {
	return firefox.ReadCookies(context.Background(), path,
		kooky.FilterFunc(func(cookie *kooky.Cookie) bool {
			domain := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
			return domain == "x.com" || strings.HasSuffix(domain, ".x.com") ||
				domain == "twitter.com" || strings.HasSuffix(domain, ".twitter.com")
		}),
		kooky.FilterFunc(func(cookie *kooky.Cookie) bool {
			return cookie.Name == "auth_token" || cookie.Name == "ct0"
		}),
	)
}

func importGeckoSession(b geckoBrowser) (*LoginResult, string, error) {
	dbs := b.cookieDBs()
	if len(dbs) == 0 {
		return nil, "", fmt.Errorf("%s has no cookie database; is it installed and set up?", b.name)
	}

	var lastErr error
	var best *LoginResult
	bestBrowser := b.name
	reader := b.reader
	if reader == nil {
		reader = readGeckoXCookies
	}
	for _, db := range dbs {
		tmp, copied, err := copyDBAs(db, "cookies.sqlite")
		if err != nil {
			lastErr = err
			continue
		}
		cookies, readErr := reader(copied)
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
				bestBrowser = b.name
				if withinRoot(db, b.windowsRoot) {
					bestBrowser += " (Windows via WSL)"
				}
			}
		}
	}
	if best != nil {
		return best, bestBrowser, nil
	}

	if lastErr != nil {
		return nil, "", fmt.Errorf("couldn't read %s cookies: %w", b.name, lastErr)
	}
	return nil, "", fmt.Errorf("no logged-in x.com session found in %s; open x.com in it, log in, then try again", b.name)
}

func withinRoot(path, root string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sessionFromCookies(cookies []*kooky.Cookie) *LoginResult {
	now := time.Now()
	values := make([]sessionCookieValue, 0, len(cookies))
	for _, cookie := range cookies {
		if !usableSessionCookie(cookie, now) {
			continue
		}
		values = append(values, sessionCookieValue{
			name: cookie.Name, value: cookie.Value, domain: cookie.Domain,
			expires: cookie.Expires, lastUsed: cookie.Creation,
		})
	}
	return selectLoginResult(values, now)
}

type sessionCookieValue struct {
	name     string
	value    string
	domain   string
	expires  time.Time
	lastUsed time.Time
}

func selectLoginResult(values []sessionCookieValue, now time.Time) *LoginResult {
	type pair struct {
		auth   *sessionCookieValue
		ct0    *sessionCookieValue
		domain string
	}
	pairs := map[string]*pair{}
	for i := range values {
		value := &values[i]
		if value.value == "" || (!value.expires.IsZero() && !value.expires.After(now)) {
			continue
		}
		domain := cookieDomainFamily(value.domain)
		if domain == "" {
			continue
		}
		current := pairs[domain]
		if current == nil {
			current = &pair{domain: domain}
			pairs[domain] = current
		}
		switch value.name {
		case "auth_token":
			if betterSessionValue(value, current.auth) {
				current.auth = value
			}
		case "ct0":
			if betterSessionValue(value, current.ct0) {
				current.ct0 = value
			}
		}
	}
	var best *LoginResult
	for _, candidate := range pairs {
		if candidate.auth == nil || candidate.ct0 == nil {
			continue
		}
		result := &LoginResult{
			AuthToken:    candidate.auth.value,
			CT0:          candidate.ct0.value,
			CookieDomain: candidate.domain,
			ExpiresAt:    earliestExpiry(candidate.auth.expires, candidate.ct0.expires),
			LastUsedAt:   latestTime(candidate.auth.lastUsed, candidate.ct0.lastUsed),
		}
		if betterLoginResult(result, best) {
			best = result
		}
	}
	return best
}

func usableSessionCookie(cookie *kooky.Cookie, now time.Time) bool {
	if cookie == nil || cookie.Value == "" || cookie.MaxAge < 0 {
		return false
	}
	return cookie.Expires.IsZero() || cookie.Expires.After(now)
}

func cookieDomainFamily(domain string) string {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	switch {
	case domain == "x.com" || strings.HasSuffix(domain, ".x.com"):
		return "x.com"
	case domain == "twitter.com" || strings.HasSuffix(domain, ".twitter.com"):
		return "twitter.com"
	default:
		return ""
	}
}

func betterSessionValue(candidate, current *sessionCookieValue) bool {
	if current == nil {
		return true
	}
	if !candidate.lastUsed.Equal(current.lastUsed) {
		return candidate.lastUsed.After(current.lastUsed)
	}
	if !candidate.expires.Equal(current.expires) {
		if candidate.expires.IsZero() {
			return true
		}
		if current.expires.IsZero() {
			return false
		}
		return candidate.expires.After(current.expires)
	}
	if candidate.domain != current.domain {
		return candidate.domain < current.domain
	}
	return candidate.value < current.value
}

func betterLoginResult(candidate, current *LoginResult) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}
	if candidate.CookieDomain != current.CookieDomain {
		if candidate.CookieDomain == "x.com" {
			return true
		}
		if current.CookieDomain == "x.com" {
			return false
		}
	}
	if !candidate.LastUsedAt.Equal(current.LastUsedAt) {
		return candidate.LastUsedAt.After(current.LastUsedAt)
	}
	if !candidate.ExpiresAt.Equal(current.ExpiresAt) {
		if candidate.ExpiresAt.IsZero() {
			return true
		}
		if current.ExpiresAt.IsZero() {
			return false
		}
		return candidate.ExpiresAt.After(current.ExpiresAt)
	}
	return candidate.Profile < current.Profile
}

func earliestExpiry(first, second time.Time) time.Time {
	if first.IsZero() {
		return second
	}
	if second.IsZero() || first.Before(second) {
		return first
	}
	return second
}

func latestTime(first, second time.Time) time.Time {
	if first.After(second) {
		return first
	}
	return second
}

func cookieProfile(dbPath string) string {
	dir := filepath.Dir(dbPath)
	if filepath.Base(dir) == "Network" {
		dir = filepath.Dir(dir)
	}
	profile := filepath.Base(dir)
	if profile == "." || profile == string(filepath.Separator) || profile == "" {
		return "unknown"
	}
	return profile
}

func findGeckoBrowser(name string, browsers []geckoBrowser) (geckoBrowser, bool) {
	for _, browser := range browsers {
		if browser.name == name {
			return browser, true
		}
	}
	return geckoBrowser{}, false
}

func copyFileBytes(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}

// copyDB copies a Chromium Cookies database and its sidecars.
func copyDB(dbPath string) (tmpDir, dst string, err error) {
	return copyDBAs(dbPath, "Cookies")
}

// copyDBAs copies a live SQLite database and its WAL sidecars under a chosen
// filename. This avoids browser locks and preserves uncheckpointed cookies.
func copyDBAs(dbPath, filename string) (tmpDir, dst string, err error) {
	tmpDir, err = os.MkdirTemp("", "xeet-cookies-")
	if err != nil {
		return "", "", err
	}
	dst = filepath.Join(tmpDir, filename)
	if err := copyFileBytes(dbPath, dst); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = copyFileBytes(dbPath+suffix, dst+suffix)
	}
	return tmpDir, dst, nil
}
