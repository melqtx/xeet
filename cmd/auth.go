package cmd

import (
	"context"
	"fmt"

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
			"Open x.com in your browser (Chrome, Arc, Brave, Dia, or Edge), log in, then run 'xeet auth' again")
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

	fmt.Printf("Reading your session from %s (macOS may ask to allow Keychain access — click Allow)...\n", browserName)
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
	cfg.AuthToken = result.AuthToken
	cfg.CT0 = result.CT0
	if err := configMgr.Save(cfg); err != nil {
		return err
	}

	handle, err := api.NewWebClient(cfg).Verify(context.Background())
	if err != nil {
		fmt.Printf("✓ Connected via %s. Run `xeet` to post.\n", browser)
		return nil
	}
	fmt.Printf("✓ Connected as @%s. Run `xeet` to post.\n", handle)
	return nil
}
