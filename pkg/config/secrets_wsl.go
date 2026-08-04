package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/melqtx/xeet/internal/wsl"
)

const wslSecretTimeout = 10 * time.Second

type dataProtector interface {
	Protect(context.Context, string, []byte) ([]byte, error)
	Unprotect(context.Context, string, []byte) ([]byte, error)
}

type windowsDPAPI struct{}

func (windowsDPAPI) Protect(ctx context.Context, key string, plaintext []byte) ([]byte, error) {
	return wsl.Protect(ctx, key, plaintext)
}

func (windowsDPAPI) Unprotect(ctx context.Context, key string, ciphertext []byte) ([]byte, error) {
	return wsl.Unprotect(ctx, key, ciphertext)
}

// wslSecretStore keeps only Windows DPAPI ciphertext in the WSL filesystem.
// Plaintext crosses the Windows boundary over stdin/stdout and exists only in
// process memory.
type wslSecretStore struct {
	dir       string
	protector dataProtector
	timeout   time.Duration
}

func newWSLSecretStore(home string) *wslSecretStore {
	return &wslSecretStore{
		dir:       filepath.Join(home, ".local", "share", "xeet", "keyring"),
		protector: windowsDPAPI{},
		timeout:   wslSecretTimeout,
	}
}

func (s *wslSecretStore) Get(key string) (string, error) {
	path, err := s.path(key)
	if err != nil {
		return "", err
	}
	ciphertext, err := secureReadFile(path)
	if os.IsNotExist(err) {
		return "", ErrSecretNotFound
	}
	if err != nil {
		return "", fmt.Errorf("reading %s from Windows-backed secure storage: %w", key, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.operationTimeout())
	defer cancel()
	plaintext, err := s.protector.Unprotect(ctx, key, ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypting %s with Windows secure storage: %w", key, err)
	}
	if len(plaintext) == 0 {
		return "", ErrSecretNotFound
	}
	return string(plaintext), nil
}

func (s *wslSecretStore) Set(key, value string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if value == "" {
		return errors.New("refusing to store an empty secret")
	}
	if err := ensurePrivateDir(s.dir); err != nil {
		return fmt.Errorf("preparing Windows-backed secure storage: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.operationTimeout())
	defer cancel()
	ciphertext, err := s.protector.Protect(ctx, key, []byte(value))
	if err != nil {
		return fmt.Errorf("encrypting %s with Windows secure storage: %w", key, err)
	}
	if len(ciphertext) == 0 {
		return errors.New("windows secure storage returned empty ciphertext")
	}
	if err := atomicWriteFile(path, ciphertext); err != nil {
		return fmt.Errorf("saving %s to Windows-backed secure storage: %w", key, err)
	}
	return nil
}

func (s *wslSecretStore) Delete(key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting %s from Windows-backed secure storage: %w", key, err)
	}
	return nil
}

func (s *wslSecretStore) path(key string) (string, error) {
	switch key {
	case keyAuthToken, keyCT0, keyLegacySessionCookies:
		return filepath.Join(s.dir, key+".dpapi"), nil
	default:
		return "", fmt.Errorf("invalid secret key %q", key)
	}
}

func (s *wslSecretStore) operationTimeout() time.Duration {
	if s.timeout > 0 {
		return s.timeout
	}
	return wslSecretTimeout
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s is not a private directory", path)
	}
	return os.Chmod(path, 0700)
}
