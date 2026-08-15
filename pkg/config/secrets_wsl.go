package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

type secretBackend uint8

const (
	backendUnselected secretBackend = iota
	backendDPAPI
	backendSecretService
)

// wslCompatibleSecretStore prefers Windows DPAPI but retains Linux Secret
// Service as a complete-session fallback. This keeps Linux-browser sessions
// working when Windows interoperability is disabled and migrates an existing
// WSL keyring session only after both values have been written to DPAPI.
type wslCompatibleSecretStore struct {
	dpapi         SecretStore
	secretService SecretStore
	mu            sync.Mutex
	selected      secretBackend
}

func newWSLCompatibleSecretStore(home string) *wslCompatibleSecretStore {
	return &wslCompatibleSecretStore{
		dpapi:         newWSLSecretStore(home),
		secretService: systemKeyring{},
	}
}

type secretPairStatus struct {
	complete bool
	partial  bool
	err      error
}

func inspectSecretPair(store SecretStore) secretPairStatus {
	auth, authErr := store.Get(keyAuthToken)
	ct0, ct0Err := store.Get(keyCT0)
	authFound := authErr == nil && auth != ""
	ct0Found := ct0Err == nil && ct0 != ""
	var unexpected []error
	if authErr != nil && !errors.Is(authErr, ErrSecretNotFound) {
		unexpected = append(unexpected, authErr)
	}
	if ct0Err != nil && !errors.Is(ct0Err, ErrSecretNotFound) {
		unexpected = append(unexpected, ct0Err)
	}
	return secretPairStatus{
		complete: authFound && ct0Found,
		partial:  authFound != ct0Found,
		err:      errors.Join(unexpected...),
	}
}

func (s *wslCompatibleSecretStore) store(backend secretBackend) SecretStore {
	if backend == backendSecretService {
		return s.secretService
	}
	return s.dpapi
}

func (s *wslCompatibleSecretStore) selectForRead() (secretBackend, error) {
	dpapi := inspectSecretPair(s.dpapi)
	if dpapi.complete {
		return backendDPAPI, nil
	}
	service := inspectSecretPair(s.secretService)
	if service.complete {
		return backendSecretService, nil
	}
	if dpapi.partial {
		return backendDPAPI, nil
	}
	if service.partial {
		return backendSecretService, nil
	}
	if dpapi.err != nil {
		return backendUnselected, dpapi.err
	}
	if service.err != nil {
		// An unavailable optional fallback must not block a fresh DPAPI
		// session on a stock WSL installation without Secret Service.
		return backendUnselected, ErrSecretNotFound
	}
	return backendUnselected, ErrSecretNotFound
}

func (s *wslCompatibleSecretStore) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selected == backendUnselected {
		backend, err := s.selectForRead()
		if err != nil {
			return "", err
		}
		s.selected = backend
	}
	return s.store(s.selected).Get(key)
}

func (s *wslCompatibleSecretStore) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selected != backendUnselected {
		return s.store(s.selected).Set(key, value)
	}
	dpapiErr := s.dpapi.Set(key, value)
	if dpapiErr == nil {
		s.selected = backendDPAPI
		return nil
	}
	if err := s.secretService.Set(key, value); err != nil {
		return errors.Join(dpapiErr, err)
	}
	s.selected = backendSecretService
	return nil
}

func (s *wslCompatibleSecretStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dpapiErr := s.dpapi.Delete(key)
	serviceErr := s.secretService.Delete(key)
	s.selected = backendUnselected
	return errors.Join(dpapiErr, serviceErr)
}

// Promote moves a complete Secret Service session to DPAPI. Failure is safe:
// the original pair remains in Secret Service and continues to be selected.
func (s *wslCompatibleSecretStore) Promote(authToken, ct0 string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selected != backendSecretService || authToken == "" || ct0 == "" {
		return nil
	}
	oldAuth, authErr := s.dpapi.Get(keyAuthToken)
	oldCT0, ct0Err := s.dpapi.Get(keyCT0)
	if authErr != nil && !errors.Is(authErr, ErrSecretNotFound) {
		return authErr
	}
	if ct0Err != nil && !errors.Is(ct0Err, ErrSecretNotFound) {
		return ct0Err
	}
	restore := func() {
		if authErr == nil {
			_ = s.dpapi.Set(keyAuthToken, oldAuth)
		} else {
			_ = s.dpapi.Delete(keyAuthToken)
		}
		if ct0Err == nil {
			_ = s.dpapi.Set(keyCT0, oldCT0)
		} else {
			_ = s.dpapi.Delete(keyCT0)
		}
	}
	if err := s.dpapi.Set(keyAuthToken, authToken); err != nil {
		restore()
		return err
	}
	if err := s.dpapi.Set(keyCT0, ct0); err != nil {
		restore()
		return err
	}
	if err := errors.Join(
		s.secretService.Delete(keyAuthToken),
		s.secretService.Delete(keyCT0),
	); err != nil {
		// DPAPI now has the complete pair, so preserve that successful
		// migration and restore the fallback pair for a later cleanup retry.
		_ = s.secretService.Set(keyAuthToken, authToken)
		_ = s.secretService.Set(keyCT0, ct0)
		s.selected = backendDPAPI
		return err
	}
	s.selected = backendDPAPI
	return nil
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
