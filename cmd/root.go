package cmd

import (
	"fmt"

	"xeet/internal/tui"
	"xeet/pkg/config"

	"github.com/spf13/cobra"
)

var (
	appVersion   string
	appCommit    string
	appBuildTime string
)

var rootCmd = &cobra.Command{
	Use:   "xeet",
	Short: "Terminal interface for posting to X.com",
	RunE:  runTUI,
}

func Execute() error { return rootCmd.Execute() }

func SetVersion(version, commit, buildTime string) {
	appVersion = version
	appCommit = commit
	appBuildTime = buildTime
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
}

func runTUI(cmd *cobra.Command, args []string) error {
	configMgr, err := config.NewConfigManager()
	if err == nil {
		if cfg, loadErr := configMgr.Load(); loadErr == nil && cfg.AuthToken == "" {
			fmt.Println("Welcome to xeet! First, connect your X account:")
			fmt.Println("  xeet auth")
			return nil
		}
	}
	return tui.Run()
}
