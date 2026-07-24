package cmd

import (
	"context"
	"fmt"

	"github.com/melqtx/xeet/internal/tui"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var (
	appVersion   string
	appCommit    string
	appBuildTime string
	barebones    bool
	composeOnly  bool
)

var rootCmd = &cobra.Command{
	Use:   "xeet",
	Short: "Terminal interface for browsing and posting to X.com",
	RunE:  runRoot,
}

func Execute() error { return rootCmd.Execute() }

func ExecuteContext(ctx context.Context) error { return rootCmd.ExecuteContext(ctx) }

func SetVersion(version, commit, buildTime string) {
	appVersion = version
	appCommit = commit
	appBuildTime = buildTime
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.Flags().BoolVar(&barebones, "barebones", false, "open a text-only timeline without image previews")
	rootCmd.Flags().BoolVar(&composeOnly, "compose", false, "open only the post composer")
	rootCmd.MarkFlagsMutuallyExclusive("barebones", "compose")
}

func runRoot(cmd *cobra.Command, args []string) error {
	configMgr, err := config.NewConfigManager()
	if err == nil {
		if cfg, loadErr := configMgr.Load(); loadErr == nil && cfg.AuthToken == "" {
			fmt.Println("Welcome to xeet! First, connect your X account:")
			fmt.Println("  xeet auth")
			return nil
		}
	}

	if composeOnly {
		return tui.Run()
	}
	imageMode := "auto"
	if barebones {
		imageMode = "off"
	}
	return runTimeline(cmd.Context(), imageMode)
}
