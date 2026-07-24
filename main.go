package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
