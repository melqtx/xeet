package cmd

import (
	"context"
	"fmt"
	"io"

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
	rootTheme     string
)

var rootCmd = &cobra.Command{
	Use:   "xeet",
	Short: "Terminal interface for browsing and posting to X.com",
	Example: `  xeet                         # your timeline, with inline images
  xeet --following             # the Following feed instead of For You
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

func SetVersion(version, commit, buildTime string) {
	appVersion = version
	appCommit = commit
	appBuildTime = buildTime
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.Flags().BoolVar(&barebones, "barebones", false, "open a text-only timeline without image previews")
	rootCmd.Flags().BoolVar(&composeOnly, "compose", false, "open only the post composer")
	rootCmd.Flags().BoolVar(&rootFollowing, "following", false, "start on the Following feed instead of For You")
	rootCmd.Flags().StringVar(&rootTheme, "theme", "", "color theme for this run (see 'xeet theme')")
	rootCmd.MarkFlagsMutuallyExclusive("barebones", "compose")
	rootCmd.MarkFlagsMutuallyExclusive("compose", "following")
}

func runRoot(cmd *cobra.Command, args []string) error {
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
	return runTimeline(cmd.Context(), imageMode, rootFollowing)
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
