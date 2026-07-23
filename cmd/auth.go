package cmd

import (
	"context"
	"fmt"
	"time"

	"xeet/pkg/api"
	"xeet/pkg/config"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Connect your X account from your browser",
	RunE:  runAuth,
}

func init() {
	rootCmd.AddCommand(authCmd)
}

// runAuth connects by reading the x.com session already present in the user's
// browser, with no passwords, API keys, or login flow.
func runAuth(cmd *cobra.Command, args []string) error {
	browsers := api.SupportedBrowsers()
	sel := promptui.Select{
		Label: "Which browser is your X account in",
		Items: browsers,
	}
	_, browserName, err := sel.Run()
	if err != nil {
		return fmt.Errorf("cancelled")
	}

	fmt.Printf("Reading your session from %s (your OS may ask to unlock its keyring)...\n", browserName)
	result, browser, err := api.ImportBrowserSession(browserName)
	if err != nil {
		return err
	}

	configMgr, err := config.NewConfigManager()
	if err != nil {
		return err
	}
	cfg, err := configMgr.Load()
	if err != nil {
		cfg = &config.Config{}
	}
	// Verify before saving so a stale or wrong browser profile cannot overwrite
	// an existing working session.
	candidate := *cfg
	candidate.AuthToken = result.AuthToken
	candidate.CT0 = result.CT0
	candidate.SessionBrowser = browser
	candidate.SessionProfile = result.Profile
	candidate.SessionDomain = result.CookieDomain
	candidate.SessionExpires = result.ExpiresAt
	candidate.SessionImported = time.Now()
	ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Second)
	defer cancel()
	handle, err := api.NewWebClient(&candidate).Verify(ctx)
	if err != nil {
		return fmt.Errorf("session found in %s but X rejected verification: %w", browser, err)
	}
	if err := configMgr.Save(&candidate); err != nil {
		return err
	}

	source := browser
	if result.Profile != "" {
		source = fmt.Sprintf("%s profile %q", browser, result.Profile)
	}
	if handle != "" {
		fmt.Printf("✓ Connected as @%s via %s. Run `xeet` for your timeline or `xeet --compose` to post.\n", handle, source)
	} else {
		fmt.Printf("✓ Connected via %s. Run `xeet` for your timeline or `xeet --compose` to post.\n", source)
	}
	return nil
}
