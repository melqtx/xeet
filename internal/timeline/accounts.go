package timeline

import (
	"fmt"

	"github.com/melqtx/xeet/pkg/config"

	tea "github.com/charmbracelet/bubbletea"
)

type timelineAccountStore interface {
	Accounts() ([]config.AccountInfo, error)
	SetActive(string) error
}

var openTimelineAccountStore = func() (timelineAccountStore, error) {
	return config.NewConfigManager()
}

type accountsLoadedMsg struct {
	accounts []config.AccountInfo
	err      error
}

type accountCycledMsg struct {
	accounts []config.AccountInfo
	active   config.AccountInfo
	err      error
}

func loadAccountsCmd() tea.Cmd {
	return func() tea.Msg {
		store, err := openTimelineAccountStore()
		if err != nil {
			return accountsLoadedMsg{err: err}
		}
		accounts, err := store.Accounts()
		return accountsLoadedMsg{accounts: accounts, err: err}
	}
}

func cycleAccountCmd() tea.Cmd {
	return func() tea.Msg {
		store, err := openTimelineAccountStore()
		if err != nil {
			return accountCycledMsg{err: err}
		}
		accounts, err := store.Accounts()
		if err != nil {
			return accountCycledMsg{err: err}
		}
		if len(accounts) == 0 {
			return accountCycledMsg{err: fmt.Errorf("no saved accounts; run 'xeet auth' first")}
		}
		active := -1
		for i := range accounts {
			if accounts[i].Active {
				active = i
				break
			}
		}
		if active < 0 {
			return accountCycledMsg{err: fmt.Errorf("no active saved account")}
		}
		if len(accounts) == 1 {
			return accountCycledMsg{accounts: accounts, err: fmt.Errorf("only one account is saved")}
		}
		next := (active + 1) % len(accounts)
		if err := store.SetActive(accounts[next].UserID); err != nil {
			return accountCycledMsg{err: fmt.Errorf("switch account: %w", err)}
		}
		for i := range accounts {
			accounts[i].Active = i == next
		}
		return accountCycledMsg{accounts: accounts, active: accounts[next]}
	}
}

func accountInfoLabel(account config.AccountInfo) string {
	if account.Handle != "" {
		return "@" + account.Handle
	}
	return account.UserID
}

func (m Model) activeAccountLabel() string {
	if len(m.accounts) <= 1 {
		return ""
	}
	for _, account := range m.accounts {
		if account.Active {
			return accountInfoLabel(account)
		}
	}
	return ""
}
