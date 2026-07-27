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

var (
	authBrowser string
	authProfile string
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "connect your x account from your browser",
	Long: `Reads the x.com session out of a browser you are already signed in to. There is
no password to type and no API key to create.

The picker marks the browsers xeet can already see a session in, and whichever
one you choose is verified with X before anything is saved, so a stale profile
can never overwrite a session that works.`,
	Example: `  xeet auth                                    # pick from the browsers on this machine
  xeet auth --browser Firefox                  # skip the picker
  xeet auth --browser Chrome --profile "Profile 8"   # and a specific profile`,
	Args: cobra.NoArgs,
	RunE: runAuth,
}

func init() {
	authCmd.Flags().StringVar(&authBrowser, "browser", "", "browser to read the session from (skips the picker)")
	authCmd.Flags().StringVar(&authProfile, "profile", "", "browser profile to read, when several are signed in")
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
	return runAuthPlain(cmd.Context(), cmd.OutOrStdout(), browser, authProfile)
}

// pickScriptedSession resolves which imported session the scripted path should
// use. With several signed-in profiles and no --profile it lists them instead of
// choosing: picking the "best" silently is how a second account used to vanish.
func pickScriptedSession(sessions []api.LoginResult, browser, profile string) (*api.LoginResult, error) {
	if len(sessions) == 0 {
		return nil, fmt.Errorf("no logged-in x.com session found in %s", browser)
	}
	if profile != "" {
		for i := range sessions {
			if strings.EqualFold(sessions[i].Profile, profile) {
				return &sessions[i], nil
			}
		}
		return nil, fmt.Errorf("no x.com session in %s profile %q (found: %s)",
			browser, profile, strings.Join(sessionProfiles(sessions), ", "))
	}
	if len(sessions) > 1 {
		return nil, fmt.Errorf("%s has %d signed-in profiles; name one with --profile (%s)",
			browser, len(sessions), strings.Join(sessionProfiles(sessions), ", "))
	}
	return &sessions[0], nil
}

func sessionProfiles(sessions []api.LoginResult) []string {
	names := make([]string, 0, len(sessions))
	for i := range sessions {
		name := sessions[i].Profile
		if name == "" {
			name = "(unnamed)"
		}
		names = append(names, name)
	}
	return names
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

type authConfigStore interface {
	Load() (*config.Config, error)
	RecordViewer(userID, handle string) error
	SaveAccount(*config.Config) error
	SaveQueryIDs(*config.Config) error
}

type authSessionClient interface {
	Verify(context.Context) error
	FetchViewer(context.Context) (*api.Account, error)
	ApplyRefreshedQueryIDs(*config.Config) bool
}

// verifyAndSave checks the imported cookies against X before writing them.
// Verifying first is what keeps a stale or wrong browser profile from
// overwriting an existing working session.
func verifyAndSave(ctx context.Context, result *api.LoginResult, browser string) (connection, error) {
	configMgr, err := config.NewConfigManager()
	if err != nil {
		return connection{}, err
	}
	return verifyAndSaveWith(ctx, result, browser, configMgr, func(cfg *config.Config) authSessionClient {
		return api.NewWebClient(cfg)
	}, time.Now)
}

func verifyAndSaveWith(
	ctx context.Context,
	result *api.LoginResult,
	browser string,
	configMgr authConfigStore,
	newClient func(*config.Config) authSessionClient,
	now func() time.Time,
) (connection, error) {
	if result == nil {
		return connection{}, fmt.Errorf("no browser session was imported")
	}
	cfg, err := configMgr.Load()
	if err != nil {
		cfg = &config.Config{}
	}
	candidate := authCandidate(cfg, result, browser, now())

	verifyCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	client := newClient(&candidate)
	if err := client.Verify(verifyCtx); err != nil {
		cancel()
		return connection{}, fmt.Errorf("session found in %s but X rejected verification: %w", browser, err)
	}
	cancel()
	client.ApplyRefreshedQueryIDs(&candidate)

	var account *api.Account
	var viewerErr error
	for range 2 {
		viewerCtx, cancelViewer := context.WithTimeout(ctx, 20*time.Second)
		account, viewerErr = client.FetchViewer(viewerCtx)
		cancelViewer()
		if viewerErr == nil && account != nil && account.ID != "" {
			break
		}
		if viewerErr == nil {
			viewerErr = fmt.Errorf("x returned no account identity for this session")
		}
	}
	if viewerErr != nil || account == nil || account.ID == "" {
		return connection{}, fmt.Errorf("session verified but xeet could not identify the account; try again: %w", viewerErr)
	}

	candidate.UserID = account.ID
	candidate.Handle = account.Handle
	client.ApplyRefreshedQueryIDs(&candidate)
	if err := configMgr.RecordViewer(account.ID, account.Handle); err != nil {
		return connection{}, err
	}
	if err := configMgr.SaveAccount(&candidate); err != nil {
		return connection{}, err
	}
	if err := configMgr.SaveQueryIDs(&candidate); err != nil {
		return connection{}, err
	}
	return connection{browser: browser, profile: result.Profile, handle: account.Handle}, nil
}

func authCandidate(global *config.Config, result *api.LoginResult, browser string, imported time.Time) config.Config {
	return config.Config{
		AuthToken:                      result.AuthToken,
		CT0:                            result.CT0,
		CreateTweetQID:                 global.CreateTweetQID,
		HomeTimelineQID:                global.HomeTimelineQID,
		HomeLatestTimelineQID:          global.HomeLatestTimelineQID,
		BookmarksQID:                   global.BookmarksQID,
		SearchTimelineQID:              global.SearchTimelineQID,
		ListLatestTweetsTimelineQID:    global.ListLatestTweetsTimelineQID,
		ListsManagementPageTimelineQID: global.ListsManagementPageTimelineQID,
		FavoriteTweetQID:               global.FavoriteTweetQID,
		UnfavoriteTweetQID:             global.UnfavoriteTweetQID,
		ViewerQID:                      global.ViewerQID,
		TweetDetailQID:                 global.TweetDetailQID,
		Theme:                          global.Theme,
		Columns:                        append([]string(nil), global.Columns...),
		SessionBrowser:                 browser,
		SessionProfile:                 result.Profile,
		SessionDomain:                  result.CookieDomain,
		SessionExpires:                 result.ExpiresAt,
		SessionImported:                imported,
	}
}

// runAuthPlain is the scripted path: no picker, no spinner, one line per step.
func runAuthPlain(ctx context.Context, out io.Writer, browser, profile string) error {
	fmt.Fprintf(out, "Reading your session from %s (your OS may ask to unlock its keyring)...\n", browser)
	sessions, resolved, err := api.ImportBrowserSessions(browser)
	if err != nil {
		return err
	}
	result, err := pickScriptedSession(sessions, resolved, profile)
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
