package cmd

import (
	"github.com/melqtx/xeet/internal/timeline"

	"github.com/spf13/cobra"
)

var (
	searchImageMode string
	searchTheme     string
)

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "search posts and browse results",
	Example: `  xeet search "go tui"  # search now and browse results
  xeet search           # open the search prompt`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := applyConfiguredTheme(searchTheme); err != nil {
			return err
		}
		query := ""
		if len(args) == 1 {
			query = args[0]
		}
		return runTimeline(cmd.Context(), searchImageMode, timeline.FeedSearch, query, "")
	},
}

func init() {
	searchCmd.Flags().StringVar(&searchImageMode, "images", "auto", "image mode: auto, native, ansi, or off")
	searchCmd.Flags().StringVar(&searchTheme, "theme", "", "color theme for this run (see 'xeet theme')")
	rootCmd.AddCommand(searchCmd)
}
