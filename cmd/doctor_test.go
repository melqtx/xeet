package cmd

import (
	"strings"
	"testing"

	"xeet/pkg/config"
)

func TestSessionFingerprintIsStableAndDoesNotExposeSecrets(t *testing.T) {
	first := sessionFingerprint("auth-secret", "ct0-secret")
	second := sessionFingerprint("auth-secret", "ct0-secret")
	other := sessionFingerprint("different", "ct0-secret")
	if first != second || first == other || len(first) != 12 {
		t.Fatalf("fingerprints first=%q second=%q other=%q", first, second, other)
	}
	if strings.Contains(first, "secret") {
		t.Fatalf("fingerprint leaked input: %q", first)
	}
}

func TestSessionSource(t *testing.T) {
	cfg := &config.Config{SessionBrowser: "Chrome", SessionProfile: "Profile 2"}
	if got := sessionSource(cfg); got != "Chrome / Profile 2" {
		t.Fatalf("source=%q", got)
	}
	if got := sessionSource(&config.Config{}); !strings.Contains(got, "unknown browser") {
		t.Fatalf("legacy source=%q", got)
	}
}
