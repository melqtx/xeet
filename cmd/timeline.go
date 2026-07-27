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
	timelineBookmarks bool
	timelineListID    string
	timelineColumns   int
	timelineTheme     string
)

var timelineCmd = &cobra.Command{
	Use:   "timeline",
	Short: "browse your home timeline",
	Example: `  xeet timeline                # same as plain 'xeet'
  xeet timeline --following    # the Following feed
  xeet timeline --bookmarks    # your saved posts
  xeet timeline --list 123     # a list by id
  xeet timeline --columns 2    # two copies of the selected feed
  xeet timeline --images off   # text only, no previews`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := applyConfiguredTheme(timelineTheme); err != nil {
			return err
		}
		feed := timeline.FeedForYou
		if timelineFollowing {
			feed = timeline.FeedFollowing
		}
		if timelineBookmarks {
			feed = timeline.FeedBookmarks
		}
		if timelineListID != "" {
			feed = timeline.FeedList
		}
		return runTimeline(cmd.Context(), timelineImageMode, feed, "", timelineListID, timelineColumns)
	},
}

func init() {
	timelineCmd.Flags().StringVar(&timelineImageMode, "images", "auto", "image mode: auto, native, ansi, or off")
	timelineCmd.Flags().BoolVar(&timelineFollowing, "following", false, "start on the Following feed instead of For You")
	timelineCmd.Flags().BoolVar(&timelineBookmarks, "bookmarks", false, "start on your bookmarks feed")
	timelineCmd.Flags().StringVar(&timelineListID, "list", "", "start on the given list id")
	timelineCmd.Flags().IntVar(&timelineColumns, "columns", 1, "number of side-by-side columns (1-4)")
	timelineCmd.Flags().StringVar(&timelineTheme, "theme", "", "color theme for this run (see 'xeet theme')")
	timelineCmd.MarkFlagsMutuallyExclusive("following", "bookmarks", "list")
	rootCmd.AddCommand(timelineCmd)
}

func runTimeline(ctx context.Context, imageMode string, feed timeline.FeedKind, query, listID string, columns int) error {
	switch imageMode {
	case "auto", "native", "ansi", "off":
	default:
		return fmt.Errorf("invalid --images value %q (use auto, native, ansi, or off)", imageMode)
	}
	if feed == timeline.FeedList && listID != "" && !isNumericListID(listID) {
		return fmt.Errorf("invalid --list value %q (use a numeric list id)", listID)
	}
	if err := validateColumnCount(columns); err != nil {
		return err
	}
	for {
		action, err := timeline.Run(ctx, imageMode, feed, query, listID, columns)
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
			// An interrupt surfaces here as an error: the browser picker
			// reports its own cancellation and verification fails on the dead
			// context. Neither is worth printing as a failure.
			if err := runAuth(authInvocation, nil); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if ctx.Err() != nil {
				return nil
			}
		default:
			return nil
		}
	}
}

func validateColumnCount(columns int) error {
	if columns < 1 || columns > 4 {
		return fmt.Errorf("invalid --columns value %d (use a number from 1 to 4)", columns)
	}
	return nil
}

func isNumericListID(id string) bool {
	for i := range len(id) {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return id != ""
}
