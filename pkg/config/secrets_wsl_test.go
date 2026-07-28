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
	if _, ok := secretStoreFor(home, true).(*wslSecretStore); !ok {
		t.Fatal("WSL did not select DPAPI store")
	}
	if _, ok := secretStoreFor(home, false).(systemKeyring); !ok {
		t.Fatal("ordinary platform did not select system keyring")
	}
}
