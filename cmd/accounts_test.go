package cmd

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

type fakeAccountsStore struct {
	accounts      []config.AccountInfo
	accountReads  int
	activeWrites  []string
	removedWrites []string
}

func (f *fakeAccountsStore) Accounts() ([]config.AccountInfo, error) {
	f.accountReads++
	return append([]config.AccountInfo(nil), f.accounts...), nil
}

func (f *fakeAccountsStore) SetActive(userID string) error {
	f.activeWrites = append(f.activeWrites, userID)
	for i := range f.accounts {
		f.accounts[i].Active = f.accounts[i].UserID == userID
	}
	return nil
}

func (f *fakeAccountsStore) EraseAccount(userID string) error {
	f.removedWrites = append(f.removedWrites, userID)
	removedActive := false
	kept := f.accounts[:0]
	for _, account := range f.accounts {
		if account.UserID == userID {
			removedActive = account.Active
			continue
		}
		kept = append(kept, account)
	}
	f.accounts = kept
	if removedActive && len(f.accounts) > 0 {
		sort.Slice(f.accounts, func(i, j int) bool {
			return f.accounts[i].UserID < f.accounts[j].UserID
		})
		f.accounts[0].Active = true
	}
	return nil
}

func useFakeAccountsStore(t *testing.T, store *fakeAccountsStore) {
	t.Helper()
	previous := openAccountsStore
	openAccountsStore = func() (accountsStore, error) { return store, nil }
	t.Cleanup(func() { openAccountsStore = previous })
}

func testAccountsCommand(out *bytes.Buffer) *cobra.Command {
	command := &cobra.Command{}
	command.SetOut(out)
	return command
}

func TestAccountsListsWithoutTouchingKeyringOrNetwork(t *testing.T) {
	store := &fakeAccountsStore{accounts: []config.AccountInfo{
		{
			UserID: "42", Handle: "alice", Active: true,
			SessionBrowser: "Chrome", SessionProfile: "Default",
			SessionImported: time.Date(2026, 7, 27, 12, 0, 0, 0, time.Local),
		},
		{UserID: "84", SessionBrowser: "Firefox"},
	}}
	useFakeAccountsStore(t, store)
	var out bytes.Buffer

	if err := runAccounts(testAccountsCommand(&out), nil); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"ACTIVE  ACCOUNT  USER ID  SESSION           IMPORTED\n" +
		"*       @alice   42       Chrome / Default  2026-07-27\n" +
		"        84       84       Firefox           -\n"
	if got := out.String(); got != want {
		t.Fatalf("accounts output =\n%q\nwant\n%q", got, want)
	}
	if store.accountReads != 1 || len(store.activeWrites) != 0 || len(store.removedWrites) != 0 {
		t.Fatalf("list touched more than file-only metadata: %+v", store)
	}
}

func TestSwitchByHandleAndByUniqueUserIDPrefix(t *testing.T) {
	store := &fakeAccountsStore{accounts: []config.AccountInfo{
		{UserID: "1560376068", Handle: "Rnmtsu", Active: true},
		{UserID: "166602457", Handle: "anchangzoxxx"},
	}}
	useFakeAccountsStore(t, store)

	var handleOut bytes.Buffer
	if err := runAccountsSwitch(testAccountsCommand(&handleOut), []string{"ANCHANGZOXXX"}); err != nil {
		t.Fatal(err)
	}
	if got := store.activeWrites; len(got) != 1 || got[0] != "166602457" {
		t.Fatalf("handle switch writes = %v", got)
	}

	var prefixOut bytes.Buffer
	if err := runAccountsSwitch(testAccountsCommand(&prefixOut), []string{"15603"}); err != nil {
		t.Fatal(err)
	}
	if got := store.activeWrites; len(got) != 2 || got[1] != "1560376068" {
		t.Fatalf("prefix switch writes = %v", got)
	}

	before := len(store.activeWrites)
	err := runAccountsSwitch(testAccountsCommand(&bytes.Buffer{}), []string{"1"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") ||
		len(store.activeWrites) != before {
		t.Fatalf("ambiguous prefix returned %v and writes %v", err, store.activeWrites)
	}
}

func TestRemoveActivePromotesNextAccount(t *testing.T) {
	store := &fakeAccountsStore{accounts: []config.AccountInfo{
		{UserID: "42", Handle: "alice", Active: true},
		{UserID: "84", Handle: "bob"},
	}}
	useFakeAccountsStore(t, store)
	var out bytes.Buffer
	command := testAccountsCommand(&out)
	command.SetIn(strings.NewReader("y\n"))

	if err := runAccountsRemove(command, []string{"alice"}); err != nil {
		t.Fatal(err)
	}
	if got := store.removedWrites; len(got) != 1 || got[0] != "42" {
		t.Fatalf("removed accounts = %v", got)
	}
	if len(store.accounts) != 1 || store.accounts[0].UserID != "84" || !store.accounts[0].Active {
		t.Fatalf("remaining accounts = %+v", store.accounts)
	}
	if got := out.String(); got != "Remove active account @alice (42)? [y/N] removed account: @alice (42)\n" {
		t.Fatalf("remove output = %q", got)
	}
}

func TestRemoveDefaultsToNo(t *testing.T) {
	store := &fakeAccountsStore{accounts: []config.AccountInfo{
		{UserID: "42", Handle: "alice", Active: true},
	}}
	useFakeAccountsStore(t, store)
	var out bytes.Buffer
	command := testAccountsCommand(&out)
	command.SetIn(strings.NewReader("\n"))

	if err := runAccountsRemove(command, []string{"42"}); err != nil {
		t.Fatal(err)
	}
	if len(store.removedWrites) != 0 {
		t.Fatalf("default confirmation removed %v", store.removedWrites)
	}
}

type fakeSelectionStore struct {
	accounts     []config.AccountInfo
	sessions     map[string]*config.Config
	active       *config.Config
	activeReads  int
	accountReads []string
}

func (f *fakeSelectionStore) Accounts() ([]config.AccountInfo, error) {
	return append([]config.AccountInfo(nil), f.accounts...), nil
}

func (f *fakeSelectionStore) Load() (*config.Config, error) {
	f.activeReads++
	return f.active, nil
}

func (f *fakeSelectionStore) LoadAccount(userID string) (*config.Config, error) {
	f.accountReads = append(f.accountReads, userID)
	cfg, ok := f.sessions[userID]
	if !ok {
		return nil, fmt.Errorf("no session for %s", userID)
	}
	return cfg, nil
}

func newSelectionStore() *fakeSelectionStore {
	return &fakeSelectionStore{
		accounts: []config.AccountInfo{
			{UserID: "42", Handle: "alice", Active: true},
			{UserID: "84", Handle: "bob"},
		},
		sessions: map[string]*config.Config{
			"42": {UserID: "42", Handle: "alice", AuthToken: "alice-auth", CT0: "alice-csrf"},
			"84": {UserID: "84", Handle: "bob", AuthToken: "bob-auth", CT0: "bob-csrf"},
		},
		active: &config.Config{UserID: "42", Handle: "alice", AuthToken: "alice-auth", CT0: "alice-csrf"},
	}
}

func TestAccountSelectorNeverReadsTheActiveAccount(t *testing.T) {
	store := newSelectionStore()
	cfg, err := loadAccountSelection(store, "@bob")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthToken != "bob-auth" {
		t.Fatalf("selector resolved to %q, want bob's session", cfg.AuthToken)
	}
	if store.activeReads != 0 {
		t.Fatalf("selector consulted the active account %d times; the flag exists so it cannot", store.activeReads)
	}
	if !reflect.DeepEqual(store.accountReads, []string{"84"}) {
		t.Fatalf("loaded accounts %v, want only 84", store.accountReads)
	}
}

func TestEmptyAccountSelectorKeepsTheActiveAccount(t *testing.T) {
	store := newSelectionStore()
	cfg, err := loadAccountSelection(store, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthToken != "alice-auth" || store.activeReads != 1 || len(store.accountReads) != 0 {
		t.Fatalf("empty selector did not fall through to the active account: %+v", store)
	}
}

func TestAccountSelectorRejectsUnknownAndSessionlessAccounts(t *testing.T) {
	store := newSelectionStore()
	if _, err := loadAccountSelection(store, "@nobody"); err == nil {
		t.Fatal("unknown selector was accepted")
	} else if !strings.Contains(err.Error(), "@nobody") {
		t.Fatalf("unknown selector error does not name it: %v", err)
	}

	store.sessions["84"] = &config.Config{UserID: "84", Handle: "bob"}
	_, err := loadAccountSelection(store, "@bob")
	if err == nil {
		t.Fatal("an account with no keyring pair was accepted")
	}
	if !strings.Contains(err.Error(), "xeet auth") {
		t.Fatalf("sessionless account error lacks the recovery step: %v", err)
	}
}
