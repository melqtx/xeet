//go:build darwin

package api

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file extracts an existing x.com session directly from a locally
// installed browser's cookie store: no login request to X, no browser
// automation, so nothing here can trip X's "we've temporarily limited your
// login" anti-bot wall. You log in once in your normal browser (which X trusts)
// and xeet reads the resulting auth_token + ct0 cookies off disk.
//
// On macOS, Chromium-family browsers store cookies in a SQLite database with
// each value AES-128-CBC encrypted under a key kept in the login Keychain
// (service "<Browser> Safe Storage"). We reproduce exactly the browser's own
// decryption: PBKDF2-SHA1(keychain_password, "saltysalt", 1003) -> 16-byte key,
// IV of 16 spaces, "v10"/"v11" prefix stripped.

type chromiumBrowser struct {
	name            string
	topDir          string // browser's Application Support root; scanned for profiles
	keychainService string
	keychainAccount string
}

func chromiumBrowsers(home string) []chromiumBrowser {
	appSup := filepath.Join(home, "Library", "Application Support")
	return []chromiumBrowser{
		{"Chrome", filepath.Join(appSup, "Google", "Chrome"), "Chrome Safe Storage", "Chrome"},
		{"Helium", filepath.Join(appSup, "net.imput.helium"), "Helium Storage Key", "Helium"},
		{"Brave", filepath.Join(appSup, "BraveSoftware", "Brave-Browser"), "Brave Safe Storage", "Brave"},
	}
}

func geckoBrowsers(home string) []geckoBrowser {
	appSup := filepath.Join(home, "Library", "Application Support")
	return []geckoBrowser{
		{"Firefox", []string{filepath.Join(appSup, "Firefox", "Profiles")}},
		{"Zen", []string{filepath.Join(appSup, "zen", "Profiles")}},
	}
}

// DetectBrowsers returns the names of installed browsers that may hold a
// logged-in x.com session, in preference order. This only checks that the
// auth_token cookie row exists, with no Keychain access or decryption, so it's
// cheap and prompt-free.
func DetectBrowsers() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	chromium := chromiumBrowsers(home)
	gecko := geckoBrowsers(home)
	var names []string
	for _, name := range SupportedBrowsers() {
		if browser, ok := findChromiumBrowser(name, chromium); ok && browser.hasXSession() {
			names = append(names, name)
			continue
		}
		if browser, ok := findGeckoBrowser(name, gecko); ok && browser.hasXSession() {
			names = append(names, name)
		}
	}
	return names
}

// hasXSession reports whether any of the browser's cookie databases contains an
// x.com auth_token row (without decrypting it).
func (b chromiumBrowser) hasXSession() bool {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		// Can't inspect; fall back to "has a cookie DB at all".
		return len(b.cookieDBs()) > 0
	}
	for _, db := range b.cookieDBs() {
		dir, dst, err := copyDB(db)
		if err != nil {
			continue
		}
		out, err := exec.Command("sqlite3", dst,
			"SELECT 1 FROM cookies WHERE (host_key LIKE '%x.com' OR host_key LIKE '%twitter.com') AND name='auth_token' LIMIT 1;").Output()
		os.RemoveAll(dir)
		if err == nil && strings.TrimSpace(string(out)) == "1" {
			return true
		}
	}
	return false
}

// ImportBrowserSession reads and decrypts the x.com session from a single named
// browser (as returned by DetectBrowsers). It only touches that browser's
// Keychain item, so the user sees at most one permission prompt.
func ImportBrowserSession(name string) (*LoginResult, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", err
	}
	if browser, ok := findGeckoBrowser(name, geckoBrowsers(home)); ok {
		return importGeckoSession(browser)
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil, "", fmt.Errorf("the 'sqlite3' command isn't available (needed to read the cookie database)")
	}

	browser, ok := findChromiumBrowser(name, chromiumBrowsers(home))
	if !ok {
		return nil, "", fmt.Errorf("unknown browser %q", name)
	}

	dbs := browser.cookieDBs()
	if len(dbs) == 0 {
		return nil, "", fmt.Errorf("%s has no cookie database. Is it installed and set up?", browser.name)
	}

	key, err := browser.deriveKey()
	if err != nil {
		return nil, "", fmt.Errorf("couldn't read %s's Keychain key: %v", browser.name, err)
	}

	var best *LoginResult
	for _, db := range dbs {
		result, err := extractXCookies(db, key)
		if err != nil {
			continue
		}
		if result != nil {
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
	return nil, "", fmt.Errorf("no logged-in x.com session found in %s. Open x.com in it, log in, then try again", browser.name)
}

func findChromiumBrowser(name string, browsers []chromiumBrowser) (chromiumBrowser, bool) {
	for _, browser := range browsers {
		if browser.name == name {
			return browser, true
		}
	}
	return chromiumBrowser{}, false
}

// cookieDBs walks the browser's data dir to find every "Cookies" SQLite file,
// regardless of the exact profile layout (including Default, Profile N, and
// optional "User Data" wrappers).
func (b chromiumBrowser) cookieDBs() []string {
	var out []string
	skip := map[string]bool{
		"Cache": true, "Code Cache": true, "GPUCache": true, "DawnCache": true,
		"Service Worker": true, "IndexedDB": true, "Local Storage": true,
		"Session Storage": true, "Extensions": true, "Sessions": true,
		"File System": true, "blob_storage": true, "Crashpad": true,
		"Storage": true, "shared_proto_db": true,
	}
	root := b.topDir
	rootDepth := strings.Count(root, string(os.PathSeparator))

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip, don't abort the walk
		}
		if d.IsDir() {
			if path != root && skip[d.Name()] {
				return fs.SkipDir
			}
			if strings.Count(path, string(os.PathSeparator))-rootDepth > 3 {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == "Cookies" {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// deriveKey pulls the browser's "Safe Storage" password out of the login
// Keychain (this triggers a one-time macOS allow prompt) and derives the AES key.
func (b chromiumBrowser) deriveKey() ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-w", "-s", b.keychainService, "-a", b.keychainAccount).Output()
	if err != nil {
		// Some browsers store the item without a matching account attribute.
		out, err = exec.Command("security", "find-generic-password",
			"-w", "-s", b.keychainService).Output()
		if err != nil {
			return nil, err
		}
	}
	password := strings.TrimRight(string(out), "\n")
	if password == "" {
		return nil, fmt.Errorf("empty keychain password")
	}
	return pbkdf2.Key(sha1.New, password, []byte("saltysalt"), 1003, 16)
}

// extractXCookies reads and decrypts the x.com auth_token and ct0 cookies from
// a single Cookies database.
func extractXCookies(dbPath string, key []byte) (*LoginResult, error) {
	tmp, dst, err := copyDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	const query = "SELECT host_key, name, hex(encrypted_value), value, expires_utc, last_access_utc FROM cookies " +
		"WHERE (host_key LIKE '%x.com' OR host_key LIKE '%twitter.com') " +
		"AND name IN ('auth_token','ct0') " +
		"ORDER BY last_access_utc DESC, host_key ASC, name ASC;"
	out, err := exec.Command("sqlite3", "-separator", "|", dst, query).Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 read failed: %w", err)
	}

	var values []sessionCookieValue
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 6)
		if len(parts) < 6 {
			continue
		}
		domain, name, encHex, plain := parts[0], parts[1], parts[2], parts[3]

		value := plain
		if encHex != "" {
			if raw, decErr := hex.DecodeString(encHex); decErr == nil {
				if dec, decErr := decryptChromeValue(raw, key); decErr == nil && dec != "" {
					value = dec
				}
			}
		}
		if value == "" {
			continue
		}
		expiresRaw, _ := strconv.ParseInt(parts[4], 10, 64)
		lastUsedRaw, _ := strconv.ParseInt(parts[5], 10, 64)
		values = append(values, sessionCookieValue{
			name: name, value: value, domain: domain,
			expires: chromeCookieTime(expiresRaw), lastUsed: chromeCookieTime(lastUsedRaw),
		})
	}
	return selectLoginResult(values, time.Now()), nil
}

// Chromium stores timestamps as microseconds since 1601-01-01 UTC.
func chromeCookieTime(microseconds int64) time.Time {
	const unixEpochInChromeMicroseconds = int64(11_644_473_600_000_000)
	if microseconds <= 0 {
		return time.Time{}
	}
	unixMicroseconds := microseconds - unixEpochInChromeMicroseconds
	seconds := unixMicroseconds / 1_000_000
	nanoseconds := (unixMicroseconds % 1_000_000) * 1_000
	return time.Unix(seconds, nanoseconds).UTC()
}

// decryptChromeValue decrypts one Chromium-encrypted cookie value on macOS.
func decryptChromeValue(enc, key []byte) (string, error) {
	if len(enc) < 3 {
		return "", fmt.Errorf("value too short")
	}
	switch string(enc[:3]) {
	case "v10", "v11":
		enc = enc[3:]
	default:
		// Not encrypted with a scheme we know; treat as plaintext.
		return string(enc), nil
	}
	if len(enc) == 0 || len(enc)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext not block-aligned")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := bytes.Repeat([]byte{' '}, aes.BlockSize)
	pt := make([]byte, len(enc))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(pt, enc)

	pt, err = pkcs7Unpad(pt)
	if err != nil {
		return "", err
	}
	// Recent Chrome prepends a 32-byte SHA-256 of the cookie's domain to the
	// plaintext. If the leading bytes aren't printable, strip that prefix.
	if !isPrintableASCII(pt) && len(pt) > 32 {
		pt = pt[32:]
	}
	return string(pt), nil
}

func pkcs7Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("empty plaintext")
	}
	n := int(b[len(b)-1])
	if n == 0 || n > aes.BlockSize || n > len(b) {
		return nil, fmt.Errorf("bad padding")
	}
	return b[:len(b)-n], nil
}

func isPrintableASCII(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}
