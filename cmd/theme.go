package cmd

import (
	"fmt"
	"strings"

	"github.com/melqtx/xeet/internal/theme"
	"github.com/melqtx/xeet/internal/timeline"
	"github.com/melqtx/xeet/internal/tui"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var themeCmd = &cobra.Command{
	Use:   "theme [name]",
	Short: "List color themes or set the default",
	Long: `With no argument, lists the available themes. With a name, saves it as the
default in ~/.xeet.yaml. Themes only change colors; the layout stays the same.
Try one without saving: xeet --theme <name>`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		configMgr, err := config.NewConfigManager()
		if err != nil {
			return err
		}
		cfg, err := configMgr.Load()
		if err != nil {
			return err
		}

		if len(args) == 0 {
			current := cfg.Theme
			if current == "" {
				current = theme.DefaultName
			}
			for _, name := range theme.Names() {
				marker := "  "
				if name == current {
					marker = "* "
				}
				fmt.Println(marker + name)
			}
			fmt.Println("\nxeet theme <name> sets the default; xeet --theme <name> tries one.")
			return nil
		}

		name := args[0]
		if _, ok := theme.Named(name); !ok {
			return fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(theme.Names(), ", "))
		}
		cfg.Theme = name
		if err := configMgr.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("theme set to %s\n", name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(themeCmd)
}

// applyConfiguredTheme resolves the palette (flag beats config beats default)
// and recolors both interfaces. An unknown name fails rather than silently
// falling back.
func applyConfiguredTheme(flagValue string) error {
	name := flagValue
	if name == "" {
		if configMgr, err := config.NewConfigManager(); err == nil {
			if cfg, loadErr := configMgr.Load(); loadErr == nil {
				name = cfg.Theme
			}
		}
	}
	if name == "" {
		return nil
	}
	palette, ok := theme.Named(name)
	if !ok {
		return fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(theme.Names(), ", "))
	}
	timeline.ApplyTheme(palette)
	tui.ApplyTheme(palette)
	return nil
}
