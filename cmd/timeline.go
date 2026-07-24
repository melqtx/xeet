package cmd

import (
	"context"
	"fmt"

	"xeet/internal/timeline"
	"xeet/internal/tui"

	"github.com/spf13/cobra"
)

var (
	timelineImageMode string
	timelineThreads   bool
)

var timelineCmd = &cobra.Command{
	Use:   "timeline",
	Short: "Browse your X home timeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTimeline(cmd.Context(), timelineImageMode, timelineThreads)
	},
}

func init() {
	timelineCmd.Flags().StringVar(&timelineImageMode, "images", "auto", "image mode: auto, native, ansi, or off")
	timelineCmd.Flags().BoolVar(&timelineThreads, "threads", false, "experimental: Enter opens a post's replies")
	rootCmd.AddCommand(timelineCmd)
}

func runTimeline(ctx context.Context, imageMode string, threads ...bool) error {
	switch imageMode {
	case "auto", "native", "ansi", "off":
	default:
		return fmt.Errorf("invalid --images value %q (use auto, native, ansi, or off)", imageMode)
	}
	for {
		action, err := timeline.Run(imageMode, len(threads) > 0 && threads[0])
		if err != nil {
			return err
		}
		switch action.Kind {
		case timeline.ActionCompose:
			if err := tui.Run(); err != nil {
				return err
			}
		case timeline.ActionAuthenticate:
			authInvocation := &cobra.Command{}
			authInvocation.SetContext(ctx)
			if err := runAuth(authInvocation, nil); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}
