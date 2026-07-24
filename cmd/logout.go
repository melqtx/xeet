package cmd

import (
	"fmt"

	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "disconnect and erase the saved session",
	Long: `Removes the x.com session tokens from your OS keyring and deletes xeet's
config file. Your browser session is untouched; run 'xeet auth' to reconnect.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configMgr, err := config.NewConfigManager()
		if err != nil {
			return err
		}
		if err := configMgr.Erase(); err != nil {
			return fmt.Errorf("logout incomplete: %w", err)
		}
		fmt.Println("✓ Logged out. Session erased from keyring and config removed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
