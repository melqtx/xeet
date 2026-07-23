package main

import (
	"os"
	"xeet/cmd"
)

// Build information (set by ldflags)
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	// Set version info for cmd package to use
	cmd.SetVersion(version, commit, buildTime)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
