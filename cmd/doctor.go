package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"xeet/pkg/api"
	"xeet/pkg/config"

	"github.com/spf13/cobra"
)

var doctorOffline bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose the saved browser session without posting",
	Args:  cobra.NoArgs,
	RunE:  runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorOffline, "offline", false, "inspect local session metadata without contacting X")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
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

	fmt.Fprintln(cmd.OutOrStdout(), "saved session: present")
	fmt.Fprintf(cmd.OutOrStdout(), "fingerprint: %s\n", sessionFingerprint(cfg.AuthToken, cfg.CT0))
	fmt.Fprintf(cmd.OutOrStdout(), "source: %s\n", sessionSource(cfg))
	if cfg.SessionDomain != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "cookie domain: %s\n", cfg.SessionDomain)
	}
	if !cfg.SessionExpires.IsZero() {
		status := cfg.SessionExpires.Local().Format(time.RFC3339)
		if time.Now().After(cfg.SessionExpires) {
			status += " (expired)"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "cookie expiry: %s\n", status)
	}
	if !cfg.SessionImported.IsZero() {
		fmt.Fprintf(cmd.OutOrStdout(), "imported: %s\n", cfg.SessionImported.Local().Format(time.RFC3339))
	}

	detected := api.DetectBrowsers()
	if len(detected) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "browser sessions detected: none")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "browser sessions detected: %s\n", strings.Join(detected, ", "))
	}

	if doctorOffline {
		fmt.Fprintln(cmd.OutOrStdout(), "verification: skipped (--offline)")
		return nil
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Second)
	defer cancel()
	if _, err := api.NewWebClient(cfg).Verify(ctx); err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "verification: authenticated timeline read succeeded")
	return nil
}

func sessionFingerprint(authToken, ct0 string) string {
	sum := sha256.Sum256([]byte(authToken + "\x00" + ct0))
	return hex.EncodeToString(sum[:6])
}

func sessionSource(cfg *config.Config) string {
	browser := cfg.SessionBrowser
	if browser == "" {
		browser = "unknown browser (re-run 'xeet auth' to record it)"
	}
	if cfg.SessionProfile != "" {
		return browser + " / " + cfg.SessionProfile
	}
	return browser
}
