//go:build !darwin && !linux

package api

import "fmt"

// DetectBrowsers is not implemented on this platform yet.
func DetectBrowsers() []string { return nil }

// ImportBrowserSession is not implemented on this platform yet.
func ImportBrowserSession(name string) (*LoginResult, string, error) {
	_, _, err := ImportBrowserSessions(name)
	return nil, "", err
}

// ImportBrowserSessions is not implemented on this platform yet.
func ImportBrowserSessions(name string) ([]LoginResult, string, error) {
	return nil, "", fmt.Errorf("importing browser cookies is currently supported on macOS and Linux")
}
