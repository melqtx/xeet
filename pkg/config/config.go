package config

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

// Config resolves one account's x.com browser session together with global,
// non-secret operation ids and settings. AuthToken and CT0 live in the OS
// keyring, never on disk.
type Config struct {
	AuthToken                      string    `yaml:"-"`
	CT0                            string    `yaml:"-"`
	UserID                         string    `yaml:"-"`
	Handle                         string    `yaml:"-"`
	CreateTweetQID                 string    `yaml:"create_tweet_qid"`
	HomeTimelineQID                string    `yaml:"home_timeline_qid,omitempty"`
	HomeLatestTimelineQID          string    `yaml:"home_latest_timeline_qid,omitempty"`
	BookmarksQID                   string    `yaml:"bookmarks_qid,omitempty"`
	SearchTimelineQID              string    `yaml:"search_timeline_qid,omitempty"`
	ListLatestTweetsTimelineQID    string    `yaml:"list_latest_tweets_timeline_qid,omitempty"`
	ListsManagementPageTimelineQID string    `yaml:"lists_management_page_timeline_qid,omitempty"`
	FavoriteTweetQID               string    `yaml:"favorite_tweet_qid,omitempty"`
	UnfavoriteTweetQID             string    `yaml:"unfavorite_tweet_qid,omitempty"`
	ViewerQID                      string    `yaml:"viewer_qid,omitempty"`
	TweetDetailQID                 string    `yaml:"tweet_detail_qid,omitempty"`
	Theme                          string    `yaml:"theme,omitempty"`
	Columns                        []string  `yaml:"columns,omitempty"`
	SessionBrowser                 string    `yaml:"session_browser,omitempty"`
	SessionProfile                 string    `yaml:"session_profile,omitempty"`
	SessionDomain                  string    `yaml:"session_domain,omitempty"`
	SessionExpires                 time.Time `yaml:"session_expires,omitempty"`
	SessionImported                time.Time `yaml:"session_imported,omitempty"`
}

// keyringService is the service name xeet registers its secrets under.
const keyringService = "xeet"

const (
	currentConfigVersion    = 2
	legacyAccountID         = "legacy"
	keyLegacyAuthToken      = "auth_token"
	keyLegacyCT0            = "ct0"
	keyLegacySessionCookies = "session_cookies"
)

func authTokenKey(userID string) string { return "auth_token:" + userID }
func ct0Key(userID string) string       { return "ct0:" + userID }

// ErrSecretNotFound is returned by a SecretStore when a key has no value.
var ErrSecretNotFound = errors.New("secret not found")

// ErrSessionIncomplete means only one of the two required session values is
// present. Re-authentication is safer than trying to use a mismatched pair.
var ErrSessionIncomplete = errors.New("saved session is incomplete; run 'xeet auth' to reconnect")

// SecretStore is where session tokens live. The real implementation is the OS
// keyring; tests inject an in-memory fake.
type SecretStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(key string) (string, error) {
	v, err := keyring.Get(keyringService, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrSecretNotFound
	}
	if err != nil {
		return "", fmt.Errorf("reading %s from OS keyring: %w", key, err)
	}
	return v, nil
}

func (systemKeyring) Set(key, value string) error {
	if err := keyring.Set(keyringService, key, value); err != nil {
		return fmt.Errorf("saving %s to OS keyring: %w", key, err)
	}
	return nil
}

func (systemKeyring) Delete(key string) error {
	err := keyring.Delete(keyringService, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// fileConfig is the on-disk shape. auth_token and ct0 are only read for
// migration from the pre-keyring layout and are never written back.
type fileConfig struct {
	Version                        int                    `yaml:"version,omitempty"`
	Active                         string                 `yaml:"active,omitempty"`
	AuthToken                      string                 `yaml:"auth_token,omitempty"`
	CT0                            string                 `yaml:"ct0,omitempty"`
	CreateTweetQID                 string                 `yaml:"create_tweet_qid,omitempty"`
	HomeTimelineQID                string                 `yaml:"home_timeline_qid,omitempty"`
	HomeLatestTimelineQID          string                 `yaml:"home_latest_timeline_qid,omitempty"`
	BookmarksQID                   string                 `yaml:"bookmarks_qid,omitempty"`
	SearchTimelineQID              string                 `yaml:"search_timeline_qid,omitempty"`
	ListLatestTweetsTimelineQID    string                 `yaml:"list_latest_tweets_timeline_qid,omitempty"`
	ListsManagementPageTimelineQID string                 `yaml:"lists_management_page_timeline_qid,omitempty"`
	FavoriteTweetQID               string                 `yaml:"favorite_tweet_qid,omitempty"`
	UnfavoriteTweetQID             string                 `yaml:"unfavorite_tweet_qid,omitempty"`
	ViewerQID                      string                 `yaml:"viewer_qid,omitempty"`
	TweetDetailQID                 string                 `yaml:"tweet_detail_qid,omitempty"`
	Theme                          string                 `yaml:"theme,omitempty"`
	Columns                        []string               `yaml:"columns,omitempty"`
	SessionBrowser                 string                 `yaml:"session_browser,omitempty"`
	SessionProfile                 string                 `yaml:"session_profile,omitempty"`
	SessionDomain                  string                 `yaml:"session_domain,omitempty"`
	SessionExpires                 string                 `yaml:"session_expires,omitempty"`
	SessionImported                string                 `yaml:"session_imported,omitempty"`
	Accounts                       map[string]fileAccount `yaml:"accounts,omitempty"`
}

type fileAccount struct {
	Handle          string `yaml:"handle,omitempty"`
	SessionBrowser  string `yaml:"session_browser,omitempty"`
	SessionProfile  string `yaml:"session_profile,omitempty"`
	SessionDomain   string `yaml:"session_domain,omitempty"`
	SessionExpires  string `yaml:"session_expires,omitempty"`
	SessionImported string `yaml:"session_imported,omitempty"`
}

type ConfigManager struct {
	configPath    string
	legacyKeyPath string
	secrets       SecretStore
}

func NewConfigManager() (*ConfigManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return newConfigManagerAt(homeDir, systemKeyring{}), nil
}

// NewConfigManagerAt keeps callers that provide storage off the process home
// directory and OS keyring.
func NewConfigManagerAt(dir string, secrets SecretStore) *ConfigManager {
	return newConfigManagerAt(dir, secrets)
}

func newConfigManagerAt(dir string, secrets SecretStore) *ConfigManager {
	return &ConfigManager{
		configPath:    filepath.Join(dir, ".xeet.yaml"),
		legacyKeyPath: filepath.Join(dir, ".xeet.key"),
		secrets:       secrets,
	}
}

func (cm *ConfigManager) Load() (*Config, error) {
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
	if fc.Active == "" {
		return configFromFile(fc), nil
	}
	return cm.loadAccount(fc, fc.Active)
}

// Theme reads the saved theme name from the config file, or "" when none is
// saved. It deliberately skips the keyring: a command that only needs to know
// what colors to draw in should never provoke a keychain unlock prompt.
func (cm *ConfigManager) Theme() (string, error) {
	fc, err := cm.readFile()
	if err != nil {
		return "", err
	}
	return fc.Theme, nil
}

// Columns reads only the persisted layout. It skips the keyring so startup
// layout resolution cannot provoke a keychain prompt before the TUI starts.
func (cm *ConfigManager) Columns() ([]string, error) {
	fc, err := cm.readFile()
	if err != nil {
		return nil, err
	}
	return append([]string(nil), fc.Columns...), nil
}

// SaveColumns patches only the explicit layout setting. Loading and saving a
// full Config here could overwrite session metadata changed by another command.
func (cm *ConfigManager) SaveColumns(columns []string) error {
	fc, err := cm.readFile()
	if err != nil {
		return err
	}
	if err := writableVersion(fc); err != nil {
		return err
	}
	fc.Columns = append([]string(nil), columns...)
	return cm.writeFile(fc)
}

// Save is the version-gated full-replacement path. Commands that change one
// concern use SaveAccount, SaveQueryIDs, SaveTheme, or SaveColumns instead.
func (cm *ConfigManager) Save(config *Config) error {
	if config == nil {
		return errors.New("config is nil")
	}
	fc, err := cm.readFile()
	if err != nil {
		return err
	}
	if err := writableVersion(fc); err != nil {
		return err
	}
	if config.UserID == "" {
		if config.AuthToken != "" || config.CT0 != "" {
			return errors.New("account user id is required")
		}
		return cm.writeFile(fileConfigFor(config))
	}
	return cm.saveAccountReplacingFile(config)
}

func fileConfigFor(config *Config) *fileConfig {
	result := &fileConfig{
		Version:                        currentConfigVersion,
		Active:                         config.UserID,
		CreateTweetQID:                 config.CreateTweetQID,
		HomeTimelineQID:                config.HomeTimelineQID,
		HomeLatestTimelineQID:          config.HomeLatestTimelineQID,
		BookmarksQID:                   config.BookmarksQID,
		SearchTimelineQID:              config.SearchTimelineQID,
		ListLatestTweetsTimelineQID:    config.ListLatestTweetsTimelineQID,
		ListsManagementPageTimelineQID: config.ListsManagementPageTimelineQID,
		FavoriteTweetQID:               config.FavoriteTweetQID,
		UnfavoriteTweetQID:             config.UnfavoriteTweetQID,
		ViewerQID:                      config.ViewerQID,
		TweetDetailQID:                 config.TweetDetailQID,
		Theme:                          config.Theme,
		Columns:                        append([]string(nil), config.Columns...),
	}
	if config.UserID != "" {
		result.Accounts = map[string]fileAccount{
			config.UserID: fileAccountFor(config),
		}
	}
	return result
}

type secretSnapshot struct {
	value  string
	exists bool
}

type committedWriteError struct{ err error }

func (e *committedWriteError) Error() string { return e.err.Error() }
func (e *committedWriteError) Unwrap() error { return e.err }

func rollbackUncommitted(err error, rollback func(error) error) error {
	var committed *committedWriteError
	if errors.As(err, &committed) {
		return err
	}
	return rollback(err)
}

func (cm *ConfigManager) snapshotSecret(key string) (secretSnapshot, error) {
	value, err := cm.secrets.Get(key)
	if errors.Is(err, ErrSecretNotFound) {
		return secretSnapshot{}, nil
	}
	if err != nil {
		return secretSnapshot{}, err
	}
	return secretSnapshot{value: value, exists: true}, nil
}

func (cm *ConfigManager) restoreSecret(key string, snapshot secretSnapshot) error {
	if snapshot.exists {
		return cm.secrets.Set(key, snapshot.value)
	}
	return cm.secrets.Delete(key)
}

// EraseAll deletes every saved session, the config file, and any legacy key
// file. It keeps going on individual failures so a partial logout removes as
// much as possible.
func (cm *ConfigManager) EraseAll() error {
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if fc, err := cm.readFile(); err == nil {
		for userID := range fc.Accounts {
			keep(cm.secrets.Delete(authTokenKey(userID)))
			keep(cm.secrets.Delete(ct0Key(userID)))
		}
	} else {
		keep(err)
	}
	keep(cm.secrets.Delete(keyLegacyAuthToken))
	keep(cm.secrets.Delete(keyLegacyCT0))
	keep(cm.secrets.Delete(keyLegacySessionCookies))
	if err := os.Remove(cm.configPath); err != nil && !os.IsNotExist(err) {
		keep(err)
	}
	if err := os.Remove(cm.legacyKeyPath); err != nil && !os.IsNotExist(err) {
		keep(err)
	}
	return firstErr
}

// Erase retains the pre-v2 API for callers that explicitly want a complete
// reset. Normal logout uses EraseAccount so global preferences survive.
func (cm *ConfigManager) Erase() error { return cm.EraseAll() }

func (cm *ConfigManager) readFile() (*fileConfig, error) {
	data, err := secureReadFile(cm.configPath)
	if os.IsNotExist(err) {
		return &fileConfig{}, nil
	}
	if err != nil {
		return nil, err
	}

	var fc fileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", cm.configPath, err)
	}
	return &fc, nil
}

// writeFile writes the config atomically (temp file + rename) with 0600
// permissions, and refuses to replace a symlink.
func (cm *ConfigManager) writeFile(fc *fileConfig) error {
	if fi, err := os.Lstat(cm.configPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("config file %s is a symlink; refusing to replace it", cm.configPath)
	}

	data, err := yaml.Marshal(fc)
	if err != nil {
		return err
	}

	dir := filepath.Dir(cm.configPath)
	tmp, err := os.CreateTemp(dir, ".xeet-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op after successful rename

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, cm.configPath); err != nil {
		return err
	}
	if err := syncDirectory(dir); err != nil {
		// Rename already published the new file; rolling back its matching
		// keyring pair here would create an account entry with no session.
		return &committedWriteError{err: err}
	}
	return nil
}

// legacyDecrypt undoes the old AES-GCM-with-key-on-disk scheme, used only to
// migrate existing sessions into the keyring.
func legacyDecrypt(ciphertext string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("invalid ciphertext")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
