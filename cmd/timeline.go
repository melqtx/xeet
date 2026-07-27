package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/melqtx/xeet/internal/timeline"
	"github.com/melqtx/xeet/internal/tui"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var (
	timelineImageMode string
	timelineFollowing bool
	timelineBookmarks bool
	timelineListID    string
	timelineColumns   string
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
  xeet timeline --columns foryou,bookmarks
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
		base := columnSpecForFeed(feed, "", timelineListID)
		singleFeedFlag := cmd.Flags().Changed("following") ||
			cmd.Flags().Changed("bookmarks") || cmd.Flags().Changed("list")
		specs, err := columnSpecsForTimelineCommand(cmd, timelineColumns, base, singleFeedFlag)
		if err != nil {
			return err
		}
		return runTimeline(cmd.Context(), timelineImageMode, specs)
	},
}

func init() {
	timelineCmd.Flags().StringVar(&timelineImageMode, "images", "auto", "image mode: auto, native, ansi, or off (multi-column iTerm2/WezTerm falls back to ansi)")
	timelineCmd.Flags().BoolVar(&timelineFollowing, "following", false, "start on the Following feed instead of For You")
	timelineCmd.Flags().BoolVar(&timelineBookmarks, "bookmarks", false, "start on your bookmarks feed")
	timelineCmd.Flags().StringVar(&timelineListID, "list", "", "start on the given list id")
	timelineCmd.Flags().StringVar(&timelineColumns, "columns", "1", "column count (1-4) or feeds: foryou,following,bookmarks,list:<id>,search:<query>")
	timelineCmd.Flags().StringVar(&timelineTheme, "theme", "", "color theme for this run (see 'xeet theme')")
	timelineCmd.MarkFlagsMutuallyExclusive("following", "bookmarks", "list")
	rootCmd.AddCommand(timelineCmd)
}

func runTimeline(ctx context.Context, imageMode string, specs []timeline.ColumnSpec) error {
	switch imageMode {
	case "auto", "native", "ansi", "off":
	default:
		return fmt.Errorf("invalid --images value %q (use auto, native, ansi, or off)", imageMode)
	}
	if err := validateColumnSpecs(specs); err != nil {
		return err
	}
	for {
		action, err := timeline.Run(ctx, imageMode, specs)
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

func columnSpecsForTimelineCommand(
	cmd *cobra.Command,
	value string,
	base timeline.ColumnSpec,
	singleFeedFlag bool,
) ([]timeline.ColumnSpec, error) {
	if cmd.Flags().Changed("columns") {
		specs, ownsFeeds, err := parseColumnsFlag(value, base)
		if err != nil {
			return nil, err
		}
		if ownsFeeds && singleFeedFlag {
			return nil, fmt.Errorf("--columns feed specs cannot be used with --following, --bookmarks, or --list")
		}
		return specs, nil
	}
	if singleFeedFlag {
		return []timeline.ColumnSpec{base}, nil
	}
	mgr, err := config.NewConfigManager()
	if err != nil {
		return nil, err
	}
	saved, err := mgr.Columns()
	if err != nil {
		return nil, err
	}
	return resolveColumnSpecs(value, false, saved, base, false)
}

func resolveColumnSpecs(
	value string,
	flagSet bool,
	saved []string,
	base timeline.ColumnSpec,
	singleFeedFlag bool,
) ([]timeline.ColumnSpec, error) {
	if flagSet {
		specs, ownsFeeds, err := parseColumnsFlag(value, base)
		if err != nil {
			return nil, err
		}
		if ownsFeeds && singleFeedFlag {
			return nil, fmt.Errorf("--columns feed specs cannot be used with --following, --bookmarks, or --list")
		}
		return specs, nil
	}
	if singleFeedFlag || len(saved) == 0 {
		return []timeline.ColumnSpec{base}, nil
	}
	specs, _, err := parseColumnSpecList(strings.Join(saved, ","))
	if err != nil {
		return nil, fmt.Errorf("invalid columns config: %w", err)
	}
	return specs, nil
}

func parseColumnsFlag(value string, base timeline.ColumnSpec) ([]timeline.ColumnSpec, bool, error) {
	value = strings.TrimSpace(value)
	if isDigits(value) {
		count, err := strconv.Atoi(value)
		if err != nil || count < 1 || count > 4 {
			return nil, false, fmt.Errorf("invalid --columns value %q (use a number from 1 to 4)", value)
		}
		specs := make([]timeline.ColumnSpec, count)
		for i := range specs {
			specs[i] = base
		}
		return specs, false, nil
	}
	specs, _, err := parseColumnSpecList(value)
	if err != nil {
		return nil, true, fmt.Errorf("invalid --columns value %q (%v)", value, err)
	}
	return specs, true, nil
}

func parseColumnSpecList(value string) ([]timeline.ColumnSpec, []string, error) {
	raw := strings.Split(value, ",")
	if len(raw) < 1 || len(raw) > 4 {
		return nil, nil, fmt.Errorf("use between 1 and 4 column specs")
	}
	specs := make([]timeline.ColumnSpec, 0, len(raw))
	canonical := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		spec := timeline.ColumnSpec{}
		switch {
		case item == "foryou":
			spec.Kind = timeline.FeedForYou
		case item == "following":
			spec.Kind = timeline.FeedFollowing
		case item == "bookmarks":
			spec.Kind = timeline.FeedBookmarks
		case strings.HasPrefix(item, "list:"):
			id := strings.TrimSpace(strings.TrimPrefix(item, "list:"))
			if !isNumericListID(id) {
				return nil, nil, fmt.Errorf("list specs need a numeric id")
			}
			spec.Kind = timeline.FeedList
			spec.ListID = id
			item = "list:" + id
		case strings.HasPrefix(item, "search:"):
			query := strings.TrimSpace(strings.TrimPrefix(item, "search:"))
			if query == "" {
				return nil, nil, fmt.Errorf("search specs need a query")
			}
			spec.Kind = timeline.FeedSearch
			spec.Query = query
			item = "search:" + query
		default:
			return nil, nil, fmt.Errorf("use foryou, following, bookmarks, list:<id>, or search:<query>")
		}
		specs = append(specs, spec)
		canonical = append(canonical, item)
	}
	return specs, canonical, nil
}

func validateColumnSpecs(specs []timeline.ColumnSpec) error {
	if len(specs) < 1 || len(specs) > 4 {
		return fmt.Errorf("invalid --columns layout with %d columns (use 1 to 4)", len(specs))
	}
	for _, spec := range specs {
		switch spec.Kind {
		case timeline.FeedForYou, timeline.FeedFollowing, timeline.FeedBookmarks:
		case timeline.FeedSearch:
			if len(specs) > 1 && spec.Query == "" {
				return fmt.Errorf("invalid --columns search spec (use search:<query>)")
			}
		case timeline.FeedList:
			if spec.ListID != "" && !isNumericListID(spec.ListID) {
				return fmt.Errorf("invalid --list value %q (use a numeric list id)", spec.ListID)
			}
			if len(specs) > 1 && spec.ListID == "" {
				return fmt.Errorf("invalid --columns list spec (use list:<id>)")
			}
		default:
			return fmt.Errorf("invalid --columns feed kind %d", spec.Kind)
		}
	}
	return nil
}

func columnSpecForFeed(feed timeline.FeedKind, query, listID string) timeline.ColumnSpec {
	return timeline.ColumnSpec{Kind: feed, Query: query, ListID: listID}
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func isNumericListID(id string) bool {
	return isDigits(id)
}
