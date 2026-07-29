package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

// AccountInfo is non-secret account metadata available without opening the
// keyring or contacting X.
type AccountInfo struct {
	UserID          string
	Handle          string
	SessionBrowser  string
	SessionProfile  string
	SessionImported time.Time
	Active          bool
}

func configFromFile(fc *fileConfig) *Config {
	return &Config{
		CreateTweetQID:                 fc.CreateTweetQID,
		HomeTimelineQID:                fc.HomeTimelineQID,
		HomeLatestTimelineQID:          fc.HomeLatestTimelineQID,
		BookmarksQID:                   fc.BookmarksQID,
		SearchTimelineQID:              fc.SearchTimelineQID,
		ListLatestTweetsTimelineQID:    fc.ListLatestTweetsTimelineQID,
		ListsManagementPageTimelineQID: fc.ListsManagementPageTimelineQID,
		FavoriteTweetQID:               fc.FavoriteTweetQID,
		UnfavoriteTweetQID:             fc.UnfavoriteTweetQID,
		ViewerQID:                      fc.ViewerQID,
		TweetDetailQID:                 fc.TweetDetailQID,
		NotificationsTimelineQID:       fc.NotificationsTimelineQID,
		CreateRetweetQID:               fc.CreateRetweetQID,
		DeleteRetweetQID:               fc.DeleteRetweetQID,
		Theme:                          fc.Theme,
		Columns:                        append([]string(nil), fc.Columns...),
		RefreshInterval:                fc.RefreshInterval,
		SessionBrowser:                 fc.SessionBrowser,
		SessionProfile:                 fc.SessionProfile,
		SessionDomain:                  fc.SessionDomain,
		SessionExpires:                 parseTime(fc.SessionExpires),
		SessionImported:                parseTime(fc.SessionImported),
	}
}

func fileAccountFor(cfg *Config) fileAccount {
	account := fileAccount{
		Handle:          cfg.Handle,
		SessionBrowser:  cfg.SessionBrowser,
		SessionProfile:  cfg.SessionProfile,
		SessionDomain:   cfg.SessionDomain,
		SessionExpires:  formatTime(cfg.SessionExpires),
		SessionImported: formatTime(cfg.SessionImported),
	}
	return account
}

func applyFileAccount(cfg *Config, userID string, account fileAccount) {
	cfg.UserID = userID
	cfg.Handle = account.Handle
	cfg.SessionBrowser = account.SessionBrowser
	cfg.SessionProfile = account.SessionProfile
	cfg.SessionDomain = account.SessionDomain
	cfg.SessionExpires = parseTime(account.SessionExpires)
	cfg.SessionImported = parseTime(account.SessionImported)
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func writableVersion(fc *fileConfig) error {
	if fc.Version > currentConfigVersion {
		return fmt.Errorf("config written by a newer xeet (version %d)", fc.Version)
	}
	return nil
}

func (cm *ConfigManager) loadAccount(fc *fileConfig, userID string) (*Config, error) {
	account, ok := fc.Accounts[userID]
	if !ok {
		return nil, fmt.Errorf("saved account %q does not exist", userID)
	}
	authToken, ct0, err := cm.loadSecretPair(userID)
	if err != nil {
		return nil, err
	}
	cfg := configFromFile(fc)
	applyFileAccount(cfg, userID, account)
	cfg.AuthToken = authToken
	cfg.CT0 = ct0
	return cfg, nil
}

func (cm *ConfigManager) loadSecretPair(userID string) (string, string, error) {
	authToken, authErr := cm.secrets.Get(authTokenKey(userID))
	ct0, ct0Err := cm.secrets.Get(ct0Key(userID))
	if authErr != nil && !errors.Is(authErr, ErrSecretNotFound) {
		return "", "", authErr
	}
	if ct0Err != nil && !errors.Is(ct0Err, ErrSecretNotFound) {
		return "", "", ct0Err
	}
	if authErr != nil || ct0Err != nil || authToken == "" || ct0 == "" {
		return "", "", ErrSessionIncomplete
	}
	return authToken, ct0, nil
}

// LoadAccount resolves one saved account without changing which account is
// active.
func (cm *ConfigManager) LoadAccount(userID string) (*Config, error) {
	fc, err := cm.readFile()
	if err != nil {
		return nil, err
	}
	if fc.Version == 0 {
		migrated, migrateErr := cm.migrateV1(fc)
		if migrateErr != nil {
			return nil, migrateErr
		}
		if migrated {
			fc, err = cm.readFile()
			if err != nil {
				return nil, err
			}
		}
	}
	return cm.loadAccount(fc, userID)
}

// Accounts lists saved account metadata without reading the keyring.
func (cm *ConfigManager) Accounts() ([]AccountInfo, error) {
	fc, err := cm.readFile()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(fc.Accounts))
	for userID := range fc.Accounts {
		ids = append(ids, userID)
	}
	sort.Strings(ids)
	accounts := make([]AccountInfo, 0, len(ids))
	for _, userID := range ids {
		account := fc.Accounts[userID]
		accounts = append(accounts, AccountInfo{
			UserID:          userID,
			Handle:          account.Handle,
			SessionBrowser:  account.SessionBrowser,
			SessionProfile:  account.SessionProfile,
			SessionImported: parseTime(account.SessionImported),
			Active:          userID == fc.Active,
		})
	}
	return accounts, nil
}

// Version reads the on-disk schema version without touching the keyring.
func (cm *ConfigManager) Version() (int, error) {
	fc, err := cm.readFile()
	if err != nil {
		return 0, err
	}
	return fc.Version, nil
}

// SetActive selects an existing account without reading or rewriting secrets.
func (cm *ConfigManager) SetActive(userID string) error {
	fc, err := cm.readFile()
	if err != nil {
		return err
	}
	if err := writableVersion(fc); err != nil {
		return err
	}
	if _, ok := fc.Accounts[userID]; !ok {
		return fmt.Errorf("saved account %q does not exist", userID)
	}
	fc.Active = userID
	return cm.writeFile(fc)
}

// SaveAccount upserts one stable user-id-keyed session while preserving every
// other account and all global settings.
func (cm *ConfigManager) SaveAccount(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	if cfg.UserID == "" {
		return errors.New("account user id is required")
	}
	if cfg.AuthToken == "" || cfg.CT0 == "" {
		return ErrSessionIncomplete
	}

	fc, err := cm.readFile()
	if err != nil {
		return err
	}
	if fc.Version == 0 {
		migrated, migrateErr := cm.migrateV1(fc)
		if migrateErr != nil {
			return migrateErr
		}
		if migrated {
			fc, err = cm.readFile()
			if err != nil {
				return err
			}
		}
	}
	if err := writableVersion(fc); err != nil {
		return err
	}
	upgradeFile(fc)

	rollback, err := cm.setSecretPair(cfg.UserID, cfg.AuthToken, cfg.CT0)
	if err != nil {
		return err
	}
	if fc.Accounts == nil {
		fc.Accounts = make(map[string]fileAccount)
	}
	fc.Accounts[cfg.UserID] = fileAccountFor(cfg)
	fc.Active = cfg.UserID
	if err := cm.writeFile(fc); err != nil {
		return rollbackUncommitted(err, rollback)
	}
	_ = cm.secrets.Delete(keyLegacySessionCookies)
	return nil
}

func (cm *ConfigManager) saveAccountReplacingFile(cfg *Config) error {
	if cfg.AuthToken == "" || cfg.CT0 == "" {
		return ErrSessionIncomplete
	}
	rollback, err := cm.setSecretPair(cfg.UserID, cfg.AuthToken, cfg.CT0)
	if err != nil {
		return err
	}
	if err := cm.writeFile(fileConfigFor(cfg)); err != nil {
		return rollbackUncommitted(err, rollback)
	}
	return nil
}

func (cm *ConfigManager) setSecretPair(userID, authToken, ct0 string) (func(error) error, error) {
	authKey := authTokenKey(userID)
	ct0KeyName := ct0Key(userID)
	oldAuth, err := cm.snapshotSecret(authKey)
	if err != nil {
		return nil, err
	}
	oldCT0, err := cm.snapshotSecret(ct0KeyName)
	if err != nil {
		return nil, err
	}
	rollback := func(cause error) error {
		return errors.Join(cause, cm.restoreSecret(authKey, oldAuth), cm.restoreSecret(ct0KeyName, oldCT0))
	}
	if err := cm.secrets.Set(authKey, authToken); err != nil {
		return nil, err
	}
	if err := cm.secrets.Set(ct0KeyName, ct0); err != nil {
		return nil, rollback(err)
	}
	return rollback, nil
}

func upgradeFile(fc *fileConfig) {
	fc.Version = currentConfigVersion
	fc.AuthToken = ""
	fc.CT0 = ""
	fc.SessionBrowser = ""
	fc.SessionProfile = ""
	fc.SessionDomain = ""
	fc.SessionExpires = ""
	fc.SessionImported = ""
	if fc.Accounts == nil {
		fc.Accounts = make(map[string]fileAccount)
	}
}

// SaveQueryIDs re-reads the file so a client holding a stale Config cannot
// resurrect or remove accounts while caching freshly discovered operations.
func (cm *ConfigManager) SaveQueryIDs(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	fc, err := cm.readFile()
	if err != nil {
		return err
	}
	if err := writableVersion(fc); err != nil {
		return err
	}
	fc.CreateTweetQID = cfg.CreateTweetQID
	fc.HomeTimelineQID = cfg.HomeTimelineQID
	fc.HomeLatestTimelineQID = cfg.HomeLatestTimelineQID
	fc.BookmarksQID = cfg.BookmarksQID
	fc.SearchTimelineQID = cfg.SearchTimelineQID
	fc.ListLatestTweetsTimelineQID = cfg.ListLatestTweetsTimelineQID
	fc.ListsManagementPageTimelineQID = cfg.ListsManagementPageTimelineQID
	fc.FavoriteTweetQID = cfg.FavoriteTweetQID
	fc.UnfavoriteTweetQID = cfg.UnfavoriteTweetQID
	fc.ViewerQID = cfg.ViewerQID
	fc.TweetDetailQID = cfg.TweetDetailQID
	fc.NotificationsTimelineQID = cfg.NotificationsTimelineQID
	fc.CreateRetweetQID = cfg.CreateRetweetQID
	fc.DeleteRetweetQID = cfg.DeleteRetweetQID
	return cm.writeFile(fc)
}

// SaveTheme patches only the global palette selection.
func (cm *ConfigManager) SaveTheme(name string) error {
	fc, err := cm.readFile()
	if err != nil {
		return err
	}
	if err := writableVersion(fc); err != nil {
		return err
	}
	fc.Theme = name
	return cm.writeFile(fc)
}

// RekeyAccount promotes a provisional account to a stable X user id. The old
// pair is deleted only after both the new pair and the rewritten file exist.
func (cm *ConfigManager) RekeyAccount(oldID, newID string) error {
	if oldID == "" || newID == "" {
		return errors.New("old and new account user ids are required")
	}
	if oldID == newID {
		return nil
	}
	fc, err := cm.readFile()
	if err != nil {
		return err
	}
	if err := writableVersion(fc); err != nil {
		return err
	}
	oldAccount, oldExists := fc.Accounts[oldID]
	newAccount, newExists := fc.Accounts[newID]
	if !oldExists {
		if newExists {
			return errors.Join(
				cm.secrets.Delete(authTokenKey(oldID)),
				cm.secrets.Delete(ct0Key(oldID)),
			)
		}
		return fmt.Errorf("saved account %q does not exist", oldID)
	}

	winnerAccount := oldAccount
	winnerAuth, winnerCT0, err := cm.loadSecretPair(oldID)
	if err != nil {
		return err
	}
	if newExists && !parseTime(oldAccount.SessionImported).After(parseTime(newAccount.SessionImported)) {
		winnerAccount = newAccount
		winnerAuth, winnerCT0, err = cm.loadSecretPair(newID)
		if err != nil {
			return err
		}
	}

	rollback, err := cm.setSecretPair(newID, winnerAuth, winnerCT0)
	if err != nil {
		return err
	}
	fc.Accounts[newID] = winnerAccount
	delete(fc.Accounts, oldID)
	if fc.Active == oldID {
		fc.Active = newID
	}
	if err := cm.writeFile(fc); err != nil {
		return rollbackUncommitted(err, rollback)
	}
	return errors.Join(
		cm.secrets.Delete(authTokenKey(oldID)),
		cm.secrets.Delete(ct0Key(oldID)),
	)
}

// RecordViewer is the single promotion path used after a successful viewer
// fetch. Cookie fingerprints are diagnostics only; the stable X user id is
// the account key.
func (cm *ConfigManager) RecordViewer(userID, handle string) error {
	if userID == "" {
		return errors.New("viewer user id is empty")
	}
	fc, err := cm.readFile()
	if err != nil {
		return err
	}
	if fc.Active == legacyAccountID {
		if err := cm.RekeyAccount(legacyAccountID, userID); err != nil {
			return err
		}
		fc, err = cm.readFile()
		if err != nil {
			return err
		}
	}
	if err := writableVersion(fc); err != nil {
		return err
	}
	account, ok := fc.Accounts[userID]
	if !ok {
		return nil
	}
	if account.Handle == handle {
		return nil
	}
	account.Handle = handle
	fc.Accounts[userID] = account
	return cm.writeFile(fc)
}

// EraseAccount removes one session while preserving global query ids, theme,
// columns, and every other account.
func (cm *ConfigManager) EraseAccount(userID string) error {
	fc, err := cm.readFile()
	if err != nil {
		return err
	}
	if err := writableVersion(fc); err != nil {
		return err
	}
	if _, ok := fc.Accounts[userID]; !ok {
		return fmt.Errorf("saved account %q does not exist", userID)
	}
	authKey := authTokenKey(userID)
	ct0KeyName := ct0Key(userID)
	oldAuth, err := cm.snapshotSecret(authKey)
	if err != nil {
		return err
	}
	oldCT0, err := cm.snapshotSecret(ct0KeyName)
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		return errors.Join(cause, cm.restoreSecret(authKey, oldAuth), cm.restoreSecret(ct0KeyName, oldCT0))
	}
	if err := cm.secrets.Delete(authKey); err != nil {
		return err
	}
	if err := cm.secrets.Delete(ct0KeyName); err != nil {
		return rollback(err)
	}

	delete(fc.Accounts, userID)
	if fc.Active == userID {
		fc.Active = ""
		ids := make([]string, 0, len(fc.Accounts))
		for id := range fc.Accounts {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if len(ids) > 0 {
			fc.Active = ids[0]
		}
	}
	if err := cm.writeFile(fc); err != nil {
		return rollbackUncommitted(err, rollback)
	}
	return nil
}

func (cm *ConfigManager) migrateV1(fc *fileConfig) (bool, error) {
	authToken, ct0, found, err := cm.legacySession(fc)
	if err != nil || !found {
		return false, err
	}

	rollback, err := cm.setSecretPair(legacyAccountID, authToken, ct0)
	if err != nil {
		return false, fmt.Errorf("migrating legacy session: %w", err)
	}
	upgraded := *fc
	legacyAccount := fileAccount{
		SessionBrowser:  fc.SessionBrowser,
		SessionProfile:  fc.SessionProfile,
		SessionDomain:   fc.SessionDomain,
		SessionExpires:  fc.SessionExpires,
		SessionImported: fc.SessionImported,
	}
	upgradeFile(&upgraded)
	upgraded.Active = legacyAccountID
	upgraded.Accounts[legacyAccountID] = legacyAccount
	if err := cm.writeFile(&upgraded); err != nil {
		wrapped := fmt.Errorf("migrating legacy session: %w", err)
		return false, rollbackUncommitted(wrapped, rollback)
	}

	cleanupErr := errors.Join(
		cm.secrets.Delete(keyLegacyAuthToken),
		cm.secrets.Delete(keyLegacyCT0),
		cm.secrets.Delete(keyLegacySessionCookies),
	)
	if err := os.Remove(cm.legacyKeyPath); err != nil && !os.IsNotExist(err) {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if cleanupErr != nil {
		return false, fmt.Errorf("cleaning up migrated legacy session: %w", cleanupErr)
	}
	return true, nil
}

func (cm *ConfigManager) legacySession(fc *fileConfig) (string, string, bool, error) {
	authToken, authErr := cm.secrets.Get(keyLegacyAuthToken)
	ct0, ct0Err := cm.secrets.Get(keyLegacyCT0)
	if authErr != nil && !errors.Is(authErr, ErrSecretNotFound) {
		return "", "", false, authErr
	}
	if ct0Err != nil && !errors.Is(ct0Err, ErrSecretNotFound) {
		return "", "", false, ct0Err
	}
	if authErr == nil || ct0Err == nil {
		if authErr != nil || ct0Err != nil || authToken == "" || ct0 == "" {
			return "", "", false, ErrSessionIncomplete
		}
		return authToken, ct0, true, nil
	}

	if fc.AuthToken == "" && fc.CT0 == "" {
		return "", "", false, nil
	}
	if fc.AuthToken == "" || fc.CT0 == "" {
		return "", "", false, ErrSessionIncomplete
	}
	authToken = fc.AuthToken
	if key, err := secureReadFile(cm.legacyKeyPath); err == nil {
		decrypted, decryptErr := legacyDecrypt(authToken, key)
		if decryptErr != nil {
			return "", "", false, fmt.Errorf("migrating legacy session: %w (run 'xeet auth' to reconnect)", decryptErr)
		}
		authToken = decrypted
	} else if !os.IsNotExist(err) {
		return "", "", false, fmt.Errorf("migrating legacy session key: %w", err)
	}
	return authToken, fc.CT0, true, nil
}
