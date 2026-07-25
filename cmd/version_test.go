package cmd

import "testing"

func TestUnsetTreatsPlaceholdersAsMissing(t *testing.T) {
	for _, value := range []string{"", "dev", "unknown"} {
		if !unset(value) {
			t.Fatalf("unset(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"v0.1.9", "0.1.9", "nix", "abc123"} {
		if unset(value) {
			t.Fatalf("unset(%q) = true, want false", value)
		}
	}
}

// goreleaser and the nix package stamp real values through -X; the build-info
// fallback must never overwrite them with what the go tool inferred.
func TestSetVersionKeepsStampedValues(t *testing.T) {
	t.Cleanup(func() { SetVersion("dev", "unknown", "unknown") })

	SetVersion("0.1.9", "deadbeef", "2026-07-25T00:00:00Z")
	if appVersion != "0.1.9" || appCommit != "deadbeef" || appBuildTime != "2026-07-25T00:00:00Z" {
		t.Fatalf("stamped values overwritten: version=%q commit=%q built=%q",
			appVersion, appCommit, appBuildTime)
	}
}

// The placeholder-replacement path isn't covered here: a test binary carries
// no vcs.* build settings and reports "(devel)", so there is nothing for the
// fallback to read. It's verified against a real build instead.
