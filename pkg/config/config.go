package config

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

// Config is the whole of xeet's saved state: your x.com browser session plus
// one cached, non-secret value. AuthToken is the session cookie and CT0 is the
// matching CSRF token; both live in the OS keyring (macOS Keychain, Linux
// Secret Service), never on disk. CreateTweetQID caches X's rotating GraphQL
// query id and is the only thing stored in the config file.
type Config struct {
	AuthToken      string `yaml:"-"`
	CT0            string `yaml:"-"`
	CreateTweetQID string `yaml:"create_tweet_qid"`
}

// keyringService is the service name xeet registers its secrets under.
const keyringService = "xeet"

const (
	keyAuthToken = "auth_token"
	keyCT0       = "ct0"
)

// ErrSecretNotFound is returned by a SecretStore when a key has no value.
var ErrSecretNotFound = errors.New("secret not found")

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
	AuthToken      string `yaml:"auth_token,omitempty"`
	CT0            string `yaml:"ct0,omitempty"`
	CreateTweetQID string `yaml:"create_tweet_qid,omitempty"`
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

	config := &Config{CreateTweetQID: fc.CreateTweetQID}

	token, err := cm.secrets.Get(keyAuthToken)
	switch {
	case err == nil:
		config.AuthToken = token
		if ct0, err := cm.secrets.Get(keyCT0); err == nil {
			config.CT0 = ct0
		}
	case errors.Is(err, ErrSecretNotFound):
		// Nothing in the keyring. If the config file still holds tokens from
		// the old layout, migrate them into the keyring now.
		if fc.AuthToken != "" {
			if err := cm.migrateLegacy(fc, config); err != nil {
				return nil, err
			}
		}
	default:
		return nil, err
	}

	return config, nil
}

// migrateLegacy moves tokens from the pre-keyring on-disk layout (AES-GCM
// encrypted auth_token with the key sitting next to it in ~/.xeet.key, ct0 in
// plaintext) into the OS keyring, rewrites the config file without them, and
// deletes the now-useless key file.
func (cm *ConfigManager) migrateLegacy(fc *fileConfig, config *Config) error {
	token := fc.AuthToken
	if key, err := os.ReadFile(cm.legacyKeyPath); err == nil {
		decrypted, err := legacyDecrypt(token, key)
		if err != nil {
			return fmt.Errorf("migrating legacy session: %w (run 'xeet auth' to reconnect)", err)
		}
		token = decrypted
	}

	config.AuthToken = token
	config.CT0 = fc.CT0

	if err := cm.Save(config); err != nil {
		return fmt.Errorf("migrating legacy session: %w", err)
	}
	_ = os.Remove(cm.legacyKeyPath)
	return nil
}

func (cm *ConfigManager) Save(config *Config) error {
	if config.AuthToken != "" {
		if err := cm.secrets.Set(keyAuthToken, config.AuthToken); err != nil {
			return err
		}
	}
	if config.CT0 != "" {
		if err := cm.secrets.Set(keyCT0, config.CT0); err != nil {
			return err
		}
	}
	return cm.writeFile(&fileConfig{CreateTweetQID: config.CreateTweetQID})
}

// Erase deletes the saved session: keyring entries, config file, and any
// legacy key file. It keeps going on individual failures and reports the
// first one, so a partial logout still removes as much as possible.
func (cm *ConfigManager) Erase() error {
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	keep(cm.secrets.Delete(keyAuthToken))
	keep(cm.secrets.Delete(keyCT0))
	if err := os.Remove(cm.configPath); err != nil && !os.IsNotExist(err) {
		keep(err)
	}
	if err := os.Remove(cm.legacyKeyPath); err != nil && !os.IsNotExist(err) {
		keep(err)
	}
	return firstErr
}

func (cm *ConfigManager) readFile() (*fileConfig, error) {
	fi, err := os.Lstat(cm.configPath)
	if os.IsNotExist(err) {
		return &fileConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("config file %s is a symlink; refusing to follow it", cm.configPath)
	}

	data, err := os.ReadFile(cm.configPath)
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, cm.configPath)
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
