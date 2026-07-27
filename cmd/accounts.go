package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

type accountsStore interface {
	Accounts() ([]config.AccountInfo, error)
	SetActive(string) error
	EraseAccount(string) error
}

var openAccountsStore = func() (accountsStore, error) {
	return config.NewConfigManager()
}

var accountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "list and switch saved x accounts",
	Args:  cobra.NoArgs,
	RunE:  runAccounts,
}

var accountsSwitchCmd = &cobra.Command{
	Use:   "switch <handle|user-id-prefix>",
	Short: "make a saved account active",
	Args:  cobra.ExactArgs(1),
	RunE:  runAccountsSwitch,
}

var accountsRemoveCmd = &cobra.Command{
	Use:   "remove <handle|user-id-prefix>",
	Short: "remove one saved account",
	Args:  cobra.ExactArgs(1),
	RunE:  runAccountsRemove,
}

func init() {
	accountsCmd.AddCommand(accountsSwitchCmd, accountsRemoveCmd)
	rootCmd.AddCommand(accountsCmd)
}

func runAccounts(cmd *cobra.Command, args []string) error {
	store, err := openAccountsStore()
	if err != nil {
		return err
	}
	accounts, err := store.Accounts()
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no saved accounts; run 'xeet auth' to connect one")
		return nil
	}

	table := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "ACTIVE\tACCOUNT\tUSER ID\tSESSION\tIMPORTED")
	for _, account := range accounts {
		marker := ""
		if account.Active {
			marker = "*"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n",
			marker,
			accountDisplayName(account),
			account.UserID,
			accountSessionSource(account),
			accountImportedDate(account.SessionImported),
		)
	}
	return table.Flush()
}

func runAccountsSwitch(cmd *cobra.Command, args []string) error {
	store, account, err := resolveSavedAccount(args[0])
	if err != nil {
		return err
	}
	if err := store.SetActive(account.UserID); err != nil {
		return fmt.Errorf("switch account: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "active account: %s\n", accountIdentity(account))
	return nil
}

func runAccountsRemove(cmd *cobra.Command, args []string) error {
	store, account, err := resolveSavedAccount(args[0])
	if err != nil {
		return err
	}
	prefix := ""
	if account.Active {
		prefix = "active "
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Remove %saccount %s? [y/N] ", prefix, accountIdentity(account))
	confirmed, err := readConfirmation(cmd.InOrStdin())
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Fprintln(cmd.OutOrStdout(), "not removed")
		return nil
	}
	if err := store.EraseAccount(account.UserID); err != nil {
		return fmt.Errorf("remove account: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "removed account: %s\n", accountIdentity(account))
	return nil
}

// selectedAccountStore is the config surface a per-invocation --account needs:
// the roster to resolve the selector against, and the session it names.
type selectedAccountStore interface {
	Accounts() ([]config.AccountInfo, error)
	Load() (*config.Config, error)
	LoadAccount(string) (*config.Config, error)
}

// loadAccountSelection resolves --account inside the one invocation that uses
// it. Switching the active account and then acting is not equivalent: anything
// else on the machine can move `active` in between, so a caller that meant one
// account can post as another. An empty selector keeps the active account, so
// the flag only ever narrows.
func loadAccountSelection(store selectedAccountStore, selector string) (*config.Config, error) {
	if strings.TrimSpace(selector) == "" {
		return store.Load()
	}
	accounts, err := store.Accounts()
	if err != nil {
		return nil, err
	}
	account, err := matchSavedAccount(accounts, selector)
	if err != nil {
		return nil, err
	}
	cfg, err := store.LoadAccount(account.UserID)
	if err != nil {
		return nil, fmt.Errorf("load account %s: %w", accountIdentity(account), err)
	}
	if cfg.AuthToken == "" || cfg.CT0 == "" {
		return nil, fmt.Errorf("account %s has no usable session; re-run 'xeet auth' for it",
			accountIdentity(account))
	}
	return cfg, nil
}

func resolveSavedAccount(selector string) (accountsStore, config.AccountInfo, error) {
	store, err := openAccountsStore()
	if err != nil {
		return nil, config.AccountInfo{}, err
	}
	accounts, err := store.Accounts()
	if err != nil {
		return nil, config.AccountInfo{}, err
	}
	account, err := matchSavedAccount(accounts, selector)
	if err != nil {
		return nil, config.AccountInfo{}, err
	}
	return store, account, nil
}

func matchSavedAccount(accounts []config.AccountInfo, selector string) (config.AccountInfo, error) {
	selector = strings.TrimSpace(selector)
	handle := strings.TrimPrefix(selector, "@")
	var exact []config.AccountInfo
	for _, account := range accounts {
		if account.Handle != "" && strings.EqualFold(account.Handle, handle) {
			exact = append(exact, account)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return config.AccountInfo{}, ambiguousAccountError(selector, exact)
	}

	var prefixes []config.AccountInfo
	for _, account := range accounts {
		if strings.HasPrefix(account.UserID, handle) {
			prefixes = append(prefixes, account)
		}
	}
	if len(prefixes) == 1 {
		return prefixes[0], nil
	}
	if len(prefixes) > 1 {
		return config.AccountInfo{}, ambiguousAccountError(selector, prefixes)
	}
	return config.AccountInfo{}, fmt.Errorf("no saved account matches %q", selector)
}

func ambiguousAccountError(selector string, matches []config.AccountInfo) error {
	names := make([]string, len(matches))
	for i, account := range matches {
		names[i] = accountIdentity(account)
	}
	return fmt.Errorf("account selector %q is ambiguous; matches %s",
		selector, strings.Join(names, ", "))
}

func accountDisplayName(account config.AccountInfo) string {
	if account.Handle != "" {
		return "@" + account.Handle
	}
	return account.UserID
}

func accountIdentity(account config.AccountInfo) string {
	name := accountDisplayName(account)
	if name == account.UserID {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, account.UserID)
}

func accountSessionSource(account config.AccountInfo) string {
	switch {
	case account.SessionBrowser == "":
		return "-"
	case account.SessionProfile == "":
		return account.SessionBrowser
	default:
		return account.SessionBrowser + " / " + account.SessionProfile
	}
}

func accountImportedDate(imported time.Time) string {
	if imported.IsZero() {
		return "-"
	}
	return imported.Local().Format("2006-01-02")
}

func readConfirmation(in io.Reader) (bool, error) {
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(answer)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}
