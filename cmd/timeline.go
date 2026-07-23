package cmd

import (
	"fmt"

	"xeet/internal/timeline"
	"xeet/internal/tui"

	"github.com/spf13/cobra"
)

var timelineImageMode string

var timelineCmd = &cobra.Command{
	Use:   "timeline",
	Short: "Browse your X home timeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTimeline(timelineImageMode)
	},
}

func init() {
	timelineCmd.Flags().StringVar(&timelineImageMode, "images", "auto", "image mode: auto, native, ansi, or off")
	rootCmd.AddCommand(timelineCmd)
}

func runTimeline(imageMode string) error {
	switch imageMode {
	case "auto", "native", "ansi", "off":
	default:
		return fmt.Errorf("invalid --images value %q (use auto, native, ansi, or off)", imageMode)
	}
	for {
		action, err := timeline.Run(imageMode)
		if err != nil {
			return err
		}
		if action.Kind != timeline.ActionCompose {
			return nil
		}
		if err := tui.Run(); err != nil {
			return err
		}
	}
}
