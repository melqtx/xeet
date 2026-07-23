package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config is the whole of xeet's saved state: your x.com browser session.
// AuthToken is the session cookie (encrypted at rest), CT0 is the matching CSRF
// token, and CreateTweetQID caches X's rotating GraphQL query id.
type Config struct {
	AuthToken      string `mapstructure:"auth_token"`
	CT0            string `mapstructure:"ct0"`
	CreateTweetQID string `mapstructure:"create_tweet_qid"`
}

type ConfigManager struct {
	configPath string
	encKey     []byte
}

func NewConfigManager() (*ConfigManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(homeDir, ".xeet.yaml")

	// Generate or load encryption key
	keyPath := filepath.Join(homeDir, ".xeet.key")
	encKey, err := loadOrGenerateKey(keyPath)
	if err != nil {
		return nil, err
	}

	return &ConfigManager{
		configPath: configPath,
		encKey:     encKey,
	}, nil
}

func (cm *ConfigManager) Load() (*Config, error) {
	v := viper.New()
	v.SetConfigFile(cm.configPath)

	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	if config.AuthToken != "" {
		decrypted, err := cm.decrypt(config.AuthToken)
		if err != nil {
			return nil, err
		}
		config.AuthToken = decrypted
	}

	return &config, nil
}

func (cm *ConfigManager) Save(config *Config) error {
	configCopy := *config
	if configCopy.AuthToken != "" {
		encrypted, err := cm.encrypt(configCopy.AuthToken)
		if err != nil {
			return err
		}
		configCopy.AuthToken = encrypted
	}

	// Fresh viper so the file contains only the keys we set (no stale fields).
	v := viper.New()
	v.SetConfigFile(cm.configPath)
	v.Set("auth_token", configCopy.AuthToken)
	v.Set("ct0", configCopy.CT0)
	v.Set("create_tweet_qid", configCopy.CreateTweetQID)

	return v.WriteConfigAs(cm.configPath)
}

func (cm *ConfigManager) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(cm.encKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (cm *ConfigManager) decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(cm.encKey)
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

func loadOrGenerateKey(keyPath string) ([]byte, error) {
	if _, err := os.Stat(keyPath); err == nil {
		return os.ReadFile(keyPath)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}

	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, err
	}

	return key, nil
}
