package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "show which xeet this is",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("xeet %s\n", appVersion)
		if cmd.Flag("verbose").Changed || len(args) > 0 {
			fmt.Printf("Commit: %s\n", appCommit)
			fmt.Printf("Built: %s\n", appBuildTime)
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().BoolP("verbose", "v", false, "show detailed version info")
}
