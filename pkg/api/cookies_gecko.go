package api

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/browserutils/kooky"
	"github.com/browserutils/kooky/browser/firefox"
)

// geckoBrowser describes Firefox and Firefox-derived browsers. Their cookies
// are stored in plain SQLite files, so no browser keyring access is needed.
type geckoBrowser struct {
	name  string
	roots []string
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
	for _, db := range dbs {
		tmp, copied, err := copyDBAs(db, "cookies.sqlite")
		if err != nil {
			lastErr = err
			continue
		}
		cookies, readErr := readGeckoXCookies(copied)
		os.RemoveAll(tmp)
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if result := sessionFromCookies(cookies); result != nil {
			return result, b.name, nil
		}
	}

	if lastErr != nil {
		return nil, "", fmt.Errorf("couldn't read %s cookies: %w", b.name, lastErr)
	}
	return nil, "", fmt.Errorf("no logged-in x.com session found in %s; open x.com in it, log in, then try again", b.name)
}

func sessionFromCookies(cookies []*kooky.Cookie) *LoginResult {
	result := &LoginResult{}
	for _, cookie := range cookies {
		if cookie == nil || cookie.Value == "" {
			continue
		}
		switch cookie.Name {
		case "auth_token":
			result.AuthToken = cookie.Value
		case "ct0":
			result.CT0 = cookie.Value
		}
	}
	if result.AuthToken == "" || result.CT0 == "" {
		return nil
	}
	return result
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
