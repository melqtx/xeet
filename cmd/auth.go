package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/melqtx/xeet/internal/ui"
	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var authBrowser string

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "connect your x account from your browser",
	Long: `Reads the x.com session out of a browser you are already signed in to. There is
no password to type and no API key to create.

The picker marks the browsers xeet can already see a session in, and whichever
one you choose is verified with X before anything is saved, so a stale profile
can never overwrite a session that works.`,
	Example: `  xeet auth                     # pick from the browsers on this machine
  xeet auth --browser Firefox   # skip the picker`,
	Args: cobra.NoArgs,
	RunE: runAuth,
}

func init() {
	authCmd.Flags().StringVar(&authBrowser, "browser", "", "browser to read the session from (skips the picker)")
	rootCmd.AddCommand(authCmd)
}

// runAuth connects by reading the x.com session already present in the user's
// browser, with no passwords, API keys, or login flow.
func runAuth(cmd *cobra.Command, args []string) error {
	var browser string
	if authBrowser != "" {
		matched, ok := matchBrowser(authBrowser)
		if !ok {
			return fmt.Errorf("unknown browser %q (supported: %s)", authBrowser, strings.Join(api.SupportedBrowsers(), ", "))
		}
		browser = matched
	}
	if ui.Interactive() {
		return runAuthInteractive(cmd.Context(), browser)
	}
	// Nothing to draw a picker on, so the browser has to come from the flag.
	if browser == "" {
		return fmt.Errorf("no terminal to show the picker on; name a browser with --browser (%s)",
			strings.Join(api.SupportedBrowsers(), ", "))
	}
	return runAuthPlain(cmd.Context(), cmd.OutOrStdout(), browser)
}

// matchBrowser accepts any capitalization of a supported browser name.
func matchBrowser(name string) (string, bool) {
	for _, supported := range api.SupportedBrowsers() {
		if strings.EqualFold(name, supported) {
			return supported, true
		}
	}
	return "", false
}

// connection is a session that has been verified and saved.
type connection struct {
	browser string
	profile string
	handle  string
}

// source describes where the session came from, the way `xeet doctor` reports it.
func (c connection) source() string {
	if c.profile != "" {
		return fmt.Sprintf("%s profile %q", c.browser, c.profile)
	}
	return c.browser
}

// verifyAndSave checks the imported cookies against X before writing them.
// Verifying first is what keeps a stale or wrong browser profile from
// overwriting an existing working session.
func verifyAndSave(ctx context.Context, result *api.LoginResult, browser string) (connection, error) {
	configMgr, err := config.NewConfigManager()
	if err != nil {
		return connection{}, err
	}
	cfg, err := configMgr.Load()
	if err != nil {
		cfg = &config.Config{}
	}
	candidate := *cfg
	candidate.AuthToken = result.AuthToken
	candidate.CT0 = result.CT0
	candidate.SessionBrowser = browser
	candidate.SessionProfile = result.Profile
	candidate.SessionDomain = result.CookieDomain
	candidate.SessionExpires = result.ExpiresAt
	candidate.SessionImported = time.Now()

	verifyCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client := api.NewWebClient(&candidate)
	if err := client.Verify(verifyCtx); err != nil {
		return connection{}, fmt.Errorf("session found in %s but X rejected verification: %w", browser, err)
	}
	client.ApplyRefreshedQueryIDs(&candidate)

	viewerCtx, cancelViewer := context.WithTimeout(ctx, 20*time.Second)
	defer cancelViewer()
	account, err := client.FetchViewer(viewerCtx)
	if err != nil {
		return connection{}, fmt.Errorf("session verified but xeet could not identify the account: %w", err)
	}
	if err := configMgr.RecordViewer(account.ID, account.Handle); err != nil {
		return connection{}, err
	}
	candidate.UserID = account.ID
	candidate.Handle = account.Handle
	client.ApplyRefreshedQueryIDs(&candidate)
	if err := configMgr.SaveAccount(&candidate); err != nil {
		return connection{}, err
	}
	if err := configMgr.SaveQueryIDs(&candidate); err != nil {
		return connection{}, err
	}
	return connection{browser: browser, profile: result.Profile, handle: account.Handle}, nil
}

// runAuthPlain is the scripted path: no picker, no spinner, one line per step.
func runAuthPlain(ctx context.Context, out io.Writer, browser string) error {
	fmt.Fprintf(out, "Reading your session from %s (your OS may ask to unlock its keyring)...\n", browser)
	result, resolved, err := api.ImportBrowserSession(browser)
	if err != nil {
		return err
	}
	conn, err := verifyAndSave(ctx, result, resolved)
	if err != nil {
		return err
	}
	who := ""
	if conn.handle != "" {
		who = fmt.Sprintf(" as @%s", conn.handle)
	}
	fmt.Fprintf(out, "✓ Connected%s via %s. Run `xeet` for your timeline or `xeet --compose` to post.\n", who, conn.source())
	return nil
}
