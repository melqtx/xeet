package cmd

import (
	"fmt"

	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var logoutAll bool

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "disconnect and erase the saved session",
	Long: `Removes the active x.com session from your OS keyring while preserving
other accounts and global settings. Use --all to remove every account and the
config file. Your browser sessions are untouched.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		configMgr, err := config.NewConfigManager()
		if err != nil {
			return err
		}
		if logoutAll {
			if err := configMgr.EraseAll(); err != nil {
				return fmt.Errorf("logout incomplete: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓ Logged out all accounts. Sessions and config erased.")
			return nil
		}
		cfg, err := configMgr.Load()
		if err != nil {
			return err
		}
		if cfg.UserID == "" {
			return fmt.Errorf("no saved session; run 'xeet auth' first")
		}
		if err := configMgr.EraseAccount(cfg.UserID); err != nil {
			return fmt.Errorf("logout incomplete: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "✓ Logged out. Active session erased; global settings preserved.")
		return nil
	},
}

func init() {
	logoutCmd.Flags().BoolVar(&logoutAll, "all", false, "remove every saved account and the config file")
	rootCmd.AddCommand(logoutCmd)
}
