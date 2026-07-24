package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/melqtx/xeet/internal/theme"
	"github.com/melqtx/xeet/internal/timeline"
	"github.com/melqtx/xeet/internal/tui"
	"github.com/melqtx/xeet/internal/ui"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var themeList bool

var themeCmd = &cobra.Command{
	Use:   "theme [name]",
	Short: "pick a color theme, with a live preview",
	Long: `With no argument, opens a picker that previews each palette as you move
through it; enter saves the highlighted one as the default in ~/.xeet.yaml.
With a name, saves it straight away. Themes only change colors; the layout
stays the same.`,
	Example: `  xeet theme              # browse the palettes and pick one
  xeet theme nord         # save nord as the default
  xeet theme --list       # just print the names, current one marked
  xeet --theme gruvbox    # try one for a single run`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTheme,
}

func init() {
	themeCmd.Flags().BoolVar(&themeList, "list", false, "print the theme names instead of opening the picker")
	rootCmd.AddCommand(themeCmd)
}

func runTheme(cmd *cobra.Command, args []string) error {
	configMgr, err := config.NewConfigManager()
	if err != nil {
		return err
	}
	// Reading the saved name skips the keyring entirely; only saving needs the
	// rest of the config, so that is where the keychain prompt belongs.
	current, err := configMgr.Theme()
	if err != nil {
		return err
	}
	if current == "" {
		current = theme.DefaultName
	}
	out := cmd.OutOrStdout()
	styles := ui.New(paletteNamed(current))

	if len(args) == 1 {
		name := args[0]
		if _, ok := theme.Named(name); !ok {
			return unknownThemeError(name)
		}
		return saveTheme(configMgr, name, out)
	}

	// Without a terminal on both ends there is nothing to preview and nothing
	// to press enter with, so fall back to the list.
	if themeList || !ui.Interactive() {
		printThemes(out, styles, current)
		return nil
	}

	chosen, err := runThemePicker(cmd.Context(), current)
	if err != nil {
		return err
	}
	if chosen == "" || chosen == current {
		fmt.Fprintln(out, styles.Dim.Render("kept "+current))
		return nil
	}
	return saveTheme(configMgr, chosen, out)
}

// saveTheme rewrites the whole config, so it has to start from the whole
// config: saving a half-read one would drop the cached query ids and the
// session metadata `xeet doctor` reports.
func saveTheme(configMgr *config.ConfigManager, name string, out io.Writer) error {
	cfg, err := configMgr.Load()
	if err != nil {
		return err
	}
	cfg.Theme = name
	if err := configMgr.Save(cfg); err != nil {
		return err
	}
	styles := ui.New(paletteNamed(name))
	fmt.Fprintln(out, styles.OK.Render("✓ ")+styles.Body.Render("theme set to "+name)+
		styles.Dim.Render("  ·  saved in ~/.xeet.yaml"))
	return nil
}

func printThemes(out io.Writer, styles ui.Styles, current string) {
	for _, name := range theme.Names() {
		if name == current {
			fmt.Fprintln(out, styles.Accent.Render("* ")+styles.Body.Render(name))
			continue
		}
		fmt.Fprintln(out, "  "+styles.Dim.Render(name))
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, styles.Dim.Render("xeet theme <name> sets the default; xeet --theme <name> tries one."))
}

func unknownThemeError(name string) error {
	return fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(theme.Names(), ", "))
}

// paletteNamed resolves a theme name for command output, falling back to the
// default rather than failing: a bad name saved in the config should not stop
// `xeet auth` from printing.
func paletteNamed(name string) theme.Palette {
	if palette, ok := theme.Named(name); ok {
		return palette
	}
	return theme.Default()
}

// configuredStyles resolves the palette commands print in: the saved default,
// or xeet's own when nothing is saved or the config cannot be read. It reads
// the file only, never the keyring, so printing help or a picker cannot set off
// a keychain prompt.
func configuredStyles() ui.Styles {
	configMgr, err := config.NewConfigManager()
	if err != nil {
		return ui.Default()
	}
	name, err := configMgr.Theme()
	if err != nil {
		return ui.Default()
	}
	return ui.New(paletteNamed(name))
}

// applyConfiguredTheme resolves the palette (flag beats config beats default)
// and recolors both interfaces. An unknown name fails rather than silently
// falling back.
func applyConfiguredTheme(flagValue string) error {
	name := flagValue
	if name == "" {
		if configMgr, err := config.NewConfigManager(); err == nil {
			if saved, readErr := configMgr.Theme(); readErr == nil {
				name = saved
			}
		}
	}
	if name == "" {
		return nil
	}
	palette, ok := theme.Named(name)
	if !ok {
		return unknownThemeError(name)
	}
	timeline.ApplyTheme(palette)
	tui.ApplyTheme(palette)
	return nil
}
