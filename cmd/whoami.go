package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var whoamiAccount string

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "show which account is connected",
	Example: `  xeet whoami
  xeet whoami --account @alice   # check one saved account without switching`,
	Args: cobra.NoArgs,
	RunE: runWhoami,
}

func init() {
	whoamiCmd.Flags().StringVar(&whoamiAccount, "account", "", "saved account to identify (handle or user id); defaults to the active one")
	rootCmd.AddCommand(whoamiCmd)
}

func runWhoami(cmd *cobra.Command, args []string) error {
	manager, err := config.NewConfigManager()
	if err != nil {
		return err
	}
	selector, err := accountSelectorFrom(cmd, whoamiAccount)
	if err != nil {
		return err
	}
	cfg, err := loadAccountSelection(manager, selector)
	if err != nil {
		return err
	}
	if cfg.AuthToken == "" || cfg.CT0 == "" {
		return fmt.Errorf("no saved session; run 'xeet auth' first")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Second)
	defer cancel()
	client := api.NewWebClient(cfg)
	account, err := client.FetchViewer(ctx)
	if err != nil {
		return fmt.Errorf("identify saved session: %w", err)
	}
	if err := manager.RecordViewer(account.ID, account.Handle); err != nil {
		return fmt.Errorf("update saved account identity: %w", err)
	}
	if client.ApplyRefreshedQueryIDs(cfg) {
		_ = manager.SaveQueryIDs(cfg)
	}
	printAccount(cmd.OutOrStdout(), account, sessionSource(cfg))
	return nil
}

func printAccount(out io.Writer, account *api.Account, source string) {
	fmt.Fprintf(out, "account: @%s\n", account.Handle)
	if account.Name != "" {
		fmt.Fprintf(out, "name: %s\n", account.Name)
	}
	if account.ID != "" {
		fmt.Fprintf(out, "id: %s\n", account.ID)
	}
	if account.Verified {
		fmt.Fprintln(out, "verified: yes")
	}
	fmt.Fprintf(out, "session: %s\n", source)
}
