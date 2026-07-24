package cmd

import (
	"context"
	"fmt"

	"github.com/melqtx/xeet/internal/timeline"
	"github.com/melqtx/xeet/internal/tui"

	"github.com/spf13/cobra"
)

var (
	timelineImageMode string
	timelineFollowing bool
	timelineTheme     string
)

var timelineCmd = &cobra.Command{
	Use:   "timeline",
	Short: "Browse your X home timeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := applyConfiguredTheme(timelineTheme); err != nil {
			return err
		}
		return runTimeline(cmd.Context(), timelineImageMode, timelineFollowing)
	},
}

func init() {
	timelineCmd.Flags().StringVar(&timelineImageMode, "images", "auto", "image mode: auto, native, ansi, or off")
	timelineCmd.Flags().BoolVar(&timelineFollowing, "following", false, "start on the Following feed instead of For You")
	timelineCmd.Flags().StringVar(&timelineTheme, "theme", "", "color theme for this run (see 'xeet theme')")
	rootCmd.AddCommand(timelineCmd)
}

func runTimeline(ctx context.Context, imageMode string, following bool) error {
	switch imageMode {
	case "auto", "native", "ansi", "off":
	default:
		return fmt.Errorf("invalid --images value %q (use auto, native, ansi, or off)", imageMode)
	}
	for {
		action, err := timeline.Run(ctx, imageMode, following)
		if err != nil {
			return err
		}
		// An interrupt has to stop the loop rather than reopen the alt screen
		// or start an interactive browser prompt against a dead context.
		if ctx.Err() != nil {
			return nil
		}
		switch action.Kind {
		case timeline.ActionCompose:
			if err := tui.Run(ctx); err != nil {
				return err
			}
			if ctx.Err() != nil {
				return nil
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
