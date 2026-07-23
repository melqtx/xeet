package cmd

import (
	"xeet/internal/timeline"
	"xeet/internal/tui"

	"github.com/spf13/cobra"
)

var timelineCmd = &cobra.Command{
	Use:   "timeline",
	Short: "Browse your X home timeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		for {
			action, err := timeline.Run()
			if err != nil {
				return err
			}
			switch action.Kind {
			case timeline.ActionCompose:
				if err := tui.Run(); err != nil {
					return err
				}
			default:
				return nil
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(timelineCmd)
}
