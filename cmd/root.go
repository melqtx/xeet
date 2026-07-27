package cmd

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/melqtx/xeet/internal/timeline"
	"github.com/melqtx/xeet/internal/tui"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var (
	appVersion    string
	appCommit     string
	appBuildTime  string
	barebones     bool
	composeOnly   bool
	rootFollowing bool
	rootBookmarks bool
	rootListID    string
	rootColumns   int
	rootTheme     string
)

var rootCmd = &cobra.Command{
	Use:   "xeet",
	Short: "Terminal interface for browsing and posting to X.com",
	Example: `  xeet                         # your timeline, with inline images
  xeet --following             # the Following feed instead of For You
  xeet --bookmarks             # your saved posts
  xeet --list 123              # a list by id
  xeet --columns 2             # two copies of the selected feed
  xeet --compose               # just the composer
  xeet post "hello, terminal"  # post without opening anything
  xeet theme                   # pick a color theme, with a preview`,
	RunE: runRoot,
}

func Execute() error {
	tidyGeneratedCommands()
	return rootCmd.Execute()
}

func ExecuteContext(ctx context.Context) error {
	tidyGeneratedCommands()
	return rootCmd.ExecuteContext(ctx)
}

// SetVersion records the build information main.go received through -X. Only
// goreleaser and the nix package set those, so `go install github.com/melqtx/
// xeet@latest` would otherwise report "dev" forever. Fall back to the data the
// go tool stamps into every binary: the module version for `go install`, and
// the VCS revision/time for a plain `go build` in a checkout.
func SetVersion(version, commit, buildTime string) {
	appVersion = version
	appCommit = commit
	appBuildTime = buildTime

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	// A checkout built without -X reports "(devel)", which is no more useful
	// than "dev"; the VCS revision below covers that case instead.
	if unset(appVersion) && info.Main.Version != "" && info.Main.Version != "(devel)" {
		appVersion = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if unset(appCommit) {
				appCommit = setting.Value
			}
		case "vcs.time":
			if unset(appBuildTime) {
				appBuildTime = setting.Value
			}
		}
	}
}

func unset(value string) bool {
	return value == "" || value == "dev" || value == "unknown"
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.Flags().BoolVar(&barebones, "barebones", false, "open a text-only timeline without image previews")
	rootCmd.Flags().BoolVar(&composeOnly, "compose", false, "open only the post composer")
	rootCmd.Flags().BoolVar(&rootFollowing, "following", false, "start on the Following feed instead of For You")
	rootCmd.Flags().BoolVar(&rootBookmarks, "bookmarks", false, "start on your bookmarks feed")
	rootCmd.Flags().StringVar(&rootListID, "list", "", "start on the given list id")
	rootCmd.Flags().IntVar(&rootColumns, "columns", 1, "number of side-by-side columns (1-4)")
	rootCmd.Flags().StringVar(&rootTheme, "theme", "", "color theme for this run (see 'xeet theme')")
	rootCmd.MarkFlagsMutuallyExclusive("barebones", "compose")
	rootCmd.MarkFlagsMutuallyExclusive("compose", "following")
	rootCmd.MarkFlagsMutuallyExclusive("following", "bookmarks", "list")
	rootCmd.MarkFlagsMutuallyExclusive("compose", "bookmarks")
	rootCmd.MarkFlagsMutuallyExclusive("compose", "list")
}

func runRoot(cmd *cobra.Command, args []string) error {
	if err := validateColumnCount(rootColumns); err != nil {
		return err
	}
	configMgr, err := config.NewConfigManager()
	if err != nil {
		return err
	}
	// A load failure here (an incomplete keyring pair, an unreadable config)
	// carries its own recovery advice. Falling through to the timeline would
	// bury it under a generic fetch error.
	cfg, err := configMgr.Load()
	if err != nil {
		return err
	}
	if cfg.AuthToken == "" {
		printFirstRun(cmd.OutOrStdout())
		return nil
	}

	if err := applyConfiguredTheme(rootTheme); err != nil {
		return err
	}
	if composeOnly {
		return tui.Run(cmd.Context())
	}
	imageMode := "auto"
	if barebones {
		imageMode = "off"
	}
	feed := timeline.FeedForYou
	if rootFollowing {
		feed = timeline.FeedFollowing
	}
	if rootBookmarks {
		feed = timeline.FeedBookmarks
	}
	if rootListID != "" {
		feed = timeline.FeedList
	}
	return runTimeline(cmd.Context(), imageMode, feed, "", rootListID, rootColumns)
}

// printFirstRun greets someone who has not connected an account yet. It is the
// only screen they can reach, so it says what xeet needs and why.
func printFirstRun(out io.Writer) {
	s := configuredStyles()
	const width = 48
	fmt.Fprintln(out, s.RenderLogo(width))
	fmt.Fprintln(out, s.RenderMascot(width, "first time? let's connect"))
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  "+s.Accent.Bold(true).Render("xeet auth")+
		s.Dim.Render("     borrow the x.com session from your browser"))
	fmt.Fprintln(out, "  "+s.Dim.Render("no password to type, no API key to create"))
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  "+s.Body.Render("xeet --help")+s.Dim.Render("   everything else"))
}
