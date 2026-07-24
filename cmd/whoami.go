package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"xeet/pkg/api"
	"xeet/pkg/config"

	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the X account connected to the saved session",
	Args:  cobra.NoArgs,
	RunE:  runWhoami,
}

func init() { rootCmd.AddCommand(whoamiCmd) }

func runWhoami(cmd *cobra.Command, args []string) error {
	manager, err := config.NewConfigManager()
	if err != nil {
		return err
	}
	cfg, err := manager.Load()
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
	if client.ApplyRefreshedQueryIDs(cfg) {
		_ = manager.Save(cfg)
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
