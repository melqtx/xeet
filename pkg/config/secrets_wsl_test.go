package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/melqtx/xeet/internal/wsl"
)

type xorProtector struct {
	protected [][]byte
}

func (p *xorProtector) Protect(_ context.Context, _ string, plaintext []byte) ([]byte, error) {
	p.protected = append(p.protected, bytes.Clone(plaintext))
	return xorBytes(plaintext), nil
}

func (p *xorProtector) Unprotect(_ context.Context, _ string, ciphertext []byte) ([]byte, error) {
	return xorBytes(ciphertext), nil
}

func xorBytes(value []byte) []byte {
	out := bytes.Clone(value)
	for i := range out {
		out[i] ^= 0xa5
	}
	return out
}

func testWSLStore(t *testing.T, protector dataProtector) *wslSecretStore {
	t.Helper()
	return &wslSecretStore{
		dir:       filepath.Join(t.TempDir(), "keyring"),
		protector: protector,
		timeout:   time.Second,
	}
}

func TestWSLSecretStoreRoundTripLeavesOnlyCiphertext(t *testing.T) {
	protector := &xorProtector{}
	store := testWSLStore(t, protector)
	const secret = "account-level-session-material"

	if err := store.Set(keyAuthToken, secret); err != nil {
		t.Fatal(err)
	}
	path, _ := store.path(keyAuthToken)
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(onDisk, []byte(secret)) {
		t.Fatal("plaintext secret reached disk")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("secret file info = %v, err = %v", info, err)
	}
	if info, err := os.Stat(store.dir); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("secret directory info = %v, err = %v", info, err)
	}

	got, err := store.Get(keyAuthToken)
	if err != nil {
		t.Fatal(err)
	}
	if got != secret {
		t.Fatalf("Get = %q", got)
	}
	if len(protector.protected) != 1 || string(protector.protected[0]) != secret {
		t.Fatalf("protector inputs = %#v", protector.protected)
	}
}

func TestWSLSecretStoreMissingAndDelete(t *testing.T) {
	store := testWSLStore(t, &xorProtector{})
	if _, err := store.Get(keyCT0); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get missing = %v", err)
	}
	if err := store.Set(keyCT0, "csrf"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(keyCT0); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(keyCT0); err != nil {
		t.Fatalf("second Delete = %v", err)
	}
	if _, err := store.Get(keyCT0); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after Delete = %v", err)
	}
}

func TestWSLConfigEraseRemovesEveryCiphertext(t *testing.T) {
	store := testWSLStore(t, &xorProtector{})
	manager := newConfigManagerAt(t.TempDir(), store)
	if err := manager.Save(&Config{AuthToken: "auth", CT0: "csrf"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Erase(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{keyAuthToken, keyCT0, keyLegacySessionCookies} {
		path, _ := store.path(key)
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s remains after Erase: %v", key, err)
		}
	}
}

func TestWSLSecretStoreRefusesSymlink(t *testing.T) {
	store := testWSLStore(t, &xorProtector{})
	if err := ensurePrivateDir(store.dir); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	path, _ := store.path(keyAuthToken)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(keyAuthToken, "secret"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Set through symlink = %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "safe" {
		t.Fatalf("symlink target changed to %q", data)
	}
}

func TestWSLSecretStoreRefusesSymlinkDirectory(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	store := &wslSecretStore{
		dir:       filepath.Join(base, "keyring"),
		protector: &xorProtector{},
		timeout:   time.Second,
	}
	if err := os.Symlink(target, store.dir); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(keyAuthToken, "secret"); err == nil {
		t.Fatal("Set accepted a symlinked keyring directory")
	}
}

type blockingProtector struct{}

func (blockingProtector) Protect(ctx context.Context, _ string, _ []byte) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingProtector) Unprotect(ctx context.Context, _ string, _ []byte) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestWSLSecretStoreTimesOut(t *testing.T) {
	store := testWSLStore(t, blockingProtector{})
	store.timeout = time.Millisecond
	err := store.Set(keyAuthToken, "secret")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Set error = %v", err)
	}
}

func TestWSLBackendSelectionIsDeterministic(t *testing.T) {
	home := t.TempDir()
	if _, ok := secretStoreFor(home, true).(*wslCompatibleSecretStore); !ok {
		t.Fatal("WSL did not select the DPAPI-compatible fallback store")
	}
	if _, ok := secretStoreFor(home, false).(systemKeyring); !ok {
		t.Fatal("ordinary platform did not select system keyring")
	}
}

type setFailStore struct {
	*fakeStore
	err error
}

func (s *setFailStore) Set(string, string) error { return s.err }

type unavailableStore struct{ err error }

func (s unavailableStore) Get(string) (string, error) { return "", s.err }
func (s unavailableStore) Set(string, string) error   { return s.err }
func (s unavailableStore) Delete(string) error        { return nil }

func TestWSLWithoutSecretServiceUsesDPAPI(t *testing.T) {
	dpapi := newFakeStore()
	store := &wslCompatibleSecretStore{
		dpapi:         dpapi,
		secretService: unavailableStore{err: errors.New("secret service unavailable")},
	}
	manager := newConfigManagerAt(t.TempDir(), store)
	if err := manager.Save(&Config{AuthToken: "auth", CT0: "csrf"}); err != nil {
		t.Fatal(err)
	}
	if dpapi.data[keyAuthToken] != "auth" || dpapi.data[keyCT0] != "csrf" {
		t.Fatalf("DPAPI session = %#v", dpapi.data)
	}
}

func TestWSLFallsBackWhenDPAPIIsUnavailable(t *testing.T) {
	dpapi := &setFailStore{fakeStore: newFakeStore(), err: wsl.ErrUnavailable}
	service := newFakeStore()
	store := &wslCompatibleSecretStore{dpapi: dpapi, secretService: service}
	manager := newConfigManagerAt(t.TempDir(), store)

	want := &Config{AuthToken: "auth", CT0: "csrf", SessionBrowser: "Firefox"}
	if err := manager.Save(want); err != nil {
		t.Fatal(err)
	}
	if service.data[keyAuthToken] != "auth" || service.data[keyCT0] != "csrf" {
		t.Fatalf("Secret Service fallback = %#v", service.data)
	}
	got, err := manager.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthToken != want.AuthToken || got.CT0 != want.CT0 {
		t.Fatalf("Load = %+v, want session from Secret Service", got)
	}
}

func TestWSLPromotesExistingSecretServicePairToDPAPI(t *testing.T) {
	dpapi := newFakeStore()
	service := newFakeStore()
	service.data[keyAuthToken] = "legacy-auth"
	service.data[keyCT0] = "legacy-csrf"
	store := &wslCompatibleSecretStore{dpapi: dpapi, secretService: service}
	manager := newConfigManagerAt(t.TempDir(), store)

	got, err := manager.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthToken != "legacy-auth" || got.CT0 != "legacy-csrf" {
		t.Fatalf("Load = %+v, want existing Secret Service session", got)
	}
	if dpapi.data[keyAuthToken] != "legacy-auth" || dpapi.data[keyCT0] != "legacy-csrf" {
		t.Fatalf("DPAPI migration = %#v", dpapi.data)
	}
	if len(service.data) != 0 {
		t.Fatalf("Secret Service entries remain after migration: %#v", service.data)
	}
}

func TestWSLPromotionFailureKeepsSecretServicePair(t *testing.T) {
	dpapi := &setFailStore{fakeStore: newFakeStore(), err: wsl.ErrUnavailable}
	service := newFakeStore()
	service.data[keyAuthToken] = "auth"
	service.data[keyCT0] = "csrf"
	store := &wslCompatibleSecretStore{dpapi: dpapi, secretService: service}
	manager := newConfigManagerAt(t.TempDir(), store)

	got, err := manager.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthToken != "auth" || got.CT0 != "csrf" {
		t.Fatalf("Load = %+v, want Secret Service session", got)
	}
	if service.data[keyAuthToken] != "auth" || service.data[keyCT0] != "csrf" {
		t.Fatalf("failed migration damaged Secret Service: %#v", service.data)
	}
}

type deleteFailStore struct {
	*fakeStore
	failKey string
}

func (s *deleteFailStore) Delete(key string) error {
	if key == s.failKey {
		return errors.New("delete failed")
	}
	return s.fakeStore.Delete(key)
}

func TestWSLPromotionCleanupFailureKeepsACompletePairInBothStores(t *testing.T) {
	dpapi := newFakeStore()
	base := newFakeStore()
	base.data[keyAuthToken] = "auth"
	base.data[keyCT0] = "csrf"
	service := &deleteFailStore{fakeStore: base, failKey: keyCT0}
	manager := newConfigManagerAt(t.TempDir(), &wslCompatibleSecretStore{
		dpapi: dpapi, secretService: service,
	})

	if _, err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	for name, store := range map[string]*fakeStore{"DPAPI": dpapi, "SecretService": base} {
		if store.data[keyAuthToken] != "auth" || store.data[keyCT0] != "csrf" {
			t.Fatalf("%s pair after cleanup failure = %#v", name, store.data)
		}
	}
}

func TestWSLEraseDeletesBothBackends(t *testing.T) {
	dpapi := newFakeStore()
	service := newFakeStore()
	for _, store := range []*fakeStore{dpapi, service} {
		store.data[keyAuthToken] = "auth"
		store.data[keyCT0] = "csrf"
		store.data[keyLegacySessionCookies] = "legacy"
	}
	manager := newConfigManagerAt(t.TempDir(), &wslCompatibleSecretStore{
		dpapi: dpapi, secretService: service,
	})
	if err := manager.Erase(); err != nil {
		t.Fatal(err)
	}
	if len(dpapi.data) != 0 || len(service.data) != 0 {
		t.Fatalf("Erase left DPAPI=%#v SecretService=%#v", dpapi.data, service.data)
	}
}
