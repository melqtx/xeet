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
// browser — no passwords, no API keys, no login flow.
func runAuth(cmd *cobra.Command, args []string) error {
	browsers := api.DetectBrowsers()
	if len(browsers) == 0 {
		return fmt.Errorf("couldn't find a logged-in x.com session\n" +
			"Open x.com in a supported browser (Chrome, Chromium, Brave, Edge, Firefox, Zen, Arc, or Dia), log in, then run 'xeet auth' again")
	}

	// One session found: use it. More than one: ask which.
	browserName := browsers[0]
	if len(browsers) > 1 {
		sel := promptui.Select{
			Label: "Which browser is your X account in",
			Items: browsers,
		}
		_, chosen, err := sel.Run()
		if err != nil {
			return fmt.Errorf("cancelled")
		}
		browserName = chosen
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
	ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Second)
	defer cancel()
	handle, err := api.NewWebClient(&candidate).Verify(ctx)
	if err != nil {
		return fmt.Errorf("session found in %s but X rejected verification: %w", browser, err)
	}
	if err := configMgr.Save(&candidate); err != nil {
		return err
	}

	fmt.Printf("✓ Connected as @%s via %s. Run `xeet` to post.\n", handle, browser)
	return nil
}
