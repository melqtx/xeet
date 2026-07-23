//go:build !darwin

package api

import "fmt"

// DetectBrowsers is macOS-only for now.
func DetectBrowsers() []string { return nil }

// ImportBrowserSession is macOS-only for now. On other platforms the cookie
// stores use different encryption (DPAPI on Windows, libsecret/kwallet on
// Linux), which isn't implemented yet.
func ImportBrowserSession(name string) (*LoginResult, string, error) {
	return nil, "", fmt.Errorf("importing browser cookies is currently supported on macOS only")
}
