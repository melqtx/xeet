package cmd

import (
	"github.com/melqtx/xeet/internal/timeline"

	"github.com/spf13/cobra"
)

var (
	listsImageMode string
	listsTheme     string
)

var listsCmd = &cobra.Command{
	Use:     "lists",
	Short:   "pick a list and browse its posts",
	Example: `  xeet lists  # pick a list and browse it`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := applyConfiguredTheme(listsTheme); err != nil {
			return err
		}
		return runTimeline(cmd.Context(), listsImageMode, []timeline.ColumnSpec{{Kind: timeline.FeedList}})
	},
}

func init() {
	listsCmd.Flags().StringVar(&listsImageMode, "images", "auto", "image mode: auto, native, ansi, or off")
	listsCmd.Flags().StringVar(&listsTheme, "theme", "", "color theme for this run (see 'xeet theme')")
	rootCmd.AddCommand(listsCmd)
}
