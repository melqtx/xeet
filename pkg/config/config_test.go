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
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	data map[string]string
}

func newFakeStore() *fakeStore { return &fakeStore{data: map[string]string{}} }

func (f *fakeStore) Get(key string) (string, error) {
	v, ok := f.data[key]
	if !ok {
		return "", ErrSecretNotFound
	}
	return v, nil
}

func (f *fakeStore) Set(key, value string) error {
	f.data[key] = value
	return nil
}

func (f *fakeStore) Delete(key string) error {
	delete(f.data, key)
	return nil
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store := newFakeStore()
	cm := newConfigManagerAt(dir, store)

	in := &Config{
		AuthToken: "tok123", CT0: "csrf456", CreateTweetQID: "qid789",
		HomeTimelineQID: "home123", FavoriteTweetQID: "like123", UnfavoriteTweetQID: "unlike123",
		SessionBrowser: "Firefox", SessionProfile: "default-release", SessionDomain: "x.com",
		SessionExpires:  time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC),
		SessionImported: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC),
	}
	if err := cm.Save(in); err != nil {
		t.Fatal(err)
	}

	out, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if *out != *in {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", out, in)
	}
}

func TestTokensNeverTouchDisk(t *testing.T) {
	dir := t.TempDir()
	cm := newConfigManagerAt(dir, newFakeStore())

	if err := cm.Save(&Config{
		AuthToken: "supersecret", CT0: "alsosecret", CreateTweetQID: "qid",
		SessionBrowser: "Firefox", SessionProfile: "default-release",
	}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".xeet.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "supersecret") || strings.Contains(string(data), "alsosecret") {
		t.Fatalf("secret material written to config file:\n%s", data)
	}
	if !strings.Contains(string(data), "qid") {
		t.Fatalf("expected create_tweet_qid in config file, got:\n%s", data)
	}
	if !strings.Contains(string(data), "Firefox") || !strings.Contains(string(data), "default-release") {
		t.Fatalf("expected non-secret session source metadata, got:\n%s", data)
	}
}

func TestConfigFilePermissions(t *testing.T) {
	dir := t.TempDir()
	cm := newConfigManagerAt(dir, newFakeStore())

	if err := cm.Save(&Config{CreateTweetQID: "qid"}); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(filepath.Join(dir, ".xeet.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Fatalf("config file permissions = %o, want 0600", perm)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cm := newConfigManagerAt(t.TempDir(), newFakeStore())
	cfg, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if *cfg != (Config{}) {
		t.Fatalf("expected zero config, got %+v", cfg)
	}
}

func TestMalformedSessionMetadataDoesNotBreakSavedSession(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".xeet.yaml"), []byte(
		"session_browser: Chrome\nsession_expires: definitely-not-a-time\n",
	), 0600); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	store.data[keyAuthToken] = "auth"
	store.data[keyCT0] = "ct0"
	cfg, err := newConfigManagerAt(dir, store).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthToken != "auth" || cfg.SessionBrowser != "Chrome" || !cfg.SessionExpires.IsZero() {
		t.Fatalf("config=%+v", cfg)
	}
}

func TestRefusesSymlinkConfig(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	if err := os.WriteFile(target, []byte("create_tweet_qid: x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ".xeet.yaml")); err != nil {
		t.Fatal(err)
	}

	cm := newConfigManagerAt(dir, newFakeStore())
	if _, err := cm.Load(); err == nil {
		t.Fatal("Load followed a symlinked config file")
	}
	if err := cm.Save(&Config{CreateTweetQID: "y"}); err == nil {
		t.Fatal("Save replaced a symlinked config file")
	}
}

func TestLegacyMigration(t *testing.T) {
	dir := t.TempDir()

	// Reproduce the old layout: AES-GCM encrypted auth_token, key alongside,
	// ct0 in plaintext.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".xeet.key"), key, 0600); err != nil {
		t.Fatal(err)
	}
	encrypted := legacyEncrypt(t, "oldtoken", key)
	yamlBody := "auth_token: " + encrypted + "\nct0: oldct0\ncreate_tweet_qid: oldqid\n"
	if err := os.WriteFile(filepath.Join(dir, ".xeet.yaml"), []byte(yamlBody), 0600); err != nil {
		t.Fatal(err)
	}

	store := newFakeStore()
	cm := newConfigManagerAt(dir, store)

	cfg, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthToken != "oldtoken" || cfg.CT0 != "oldct0" || cfg.CreateTweetQID != "oldqid" {
		t.Fatalf("migration produced %+v", cfg)
	}

	// Tokens must now be in the keyring.
	if store.data["auth_token"] != "oldtoken" || store.data["ct0"] != "oldct0" {
		t.Fatalf("keyring after migration: %+v", store.data)
	}

	// The key file must be gone and the config file scrubbed of tokens.
	if _, err := os.Stat(filepath.Join(dir, ".xeet.key")); !os.IsNotExist(err) {
		t.Fatal("legacy key file still exists after migration")
	}
	data, err := os.ReadFile(filepath.Join(dir, ".xeet.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "oldct0") || strings.Contains(string(data), encrypted) {
		t.Fatalf("config file still holds token material after migration:\n%s", data)
	}
}

func TestErase(t *testing.T) {
	dir := t.TempDir()
	store := newFakeStore()
	cm := newConfigManagerAt(dir, store)

	if err := cm.Save(&Config{AuthToken: "tok", CT0: "ct", CreateTweetQID: "qid"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".xeet.key"), []byte("junk"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := cm.Erase(); err != nil {
		t.Fatal(err)
	}

	if len(store.data) != 0 {
		t.Fatalf("keyring not emptied: %+v", store.data)
	}
	for _, name := range []string{".xeet.yaml", ".xeet.key"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after Erase", name)
		}
	}

	// Erase on an already-clean state must succeed.
	if err := cm.Erase(); err != nil {
		t.Fatal(err)
	}
}

func legacyEncrypt(t *testing.T, plaintext string, key []byte) string {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil))
}

type failingStore struct {
	*fakeStore
	failKey   string
	failValue string
}

func (f *failingStore) Set(key, value string) error {
	if key == f.failKey && value == f.failValue {
		return errors.New("injected keyring failure")
	}
	return f.fakeStore.Set(key, value)
}

func TestSaveRollsBackPartialKeyringWrite(t *testing.T) {
	base := newFakeStore()
	base.data[keyAuthToken] = "old-auth"
	base.data[keyCT0] = "old-ct0"
	store := &failingStore{fakeStore: base, failKey: keyCT0, failValue: "new-ct0"}
	cm := newConfigManagerAt(t.TempDir(), store)

	err := cm.Save(&Config{AuthToken: "new-auth", CT0: "new-ct0", CreateTweetQID: "qid"})
	if err == nil {
		t.Fatal("expected injected keyring error")
	}
	if base.data[keyAuthToken] != "old-auth" || base.data[keyCT0] != "old-ct0" {
		t.Fatalf("partial save was not rolled back: %+v", base.data)
	}
}

func TestSaveRollsBackWhenConfigWriteFails(t *testing.T) {
	dir := t.TempDir()
	store := newFakeStore()
	store.data[keyAuthToken] = "old-auth"
	store.data[keyCT0] = "old-ct0"
	cm := newConfigManagerAt(dir, store)

	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, cm.configPath); err != nil {
		t.Fatal(err)
	}

	err := cm.Save(&Config{AuthToken: "new-auth", CT0: "new-ct0", CreateTweetQID: "qid"})
	if err == nil {
		t.Fatal("expected config write error")
	}
	if store.data[keyAuthToken] != "old-auth" || store.data[keyCT0] != "old-ct0" {
		t.Fatalf("keyring was not rolled back: %+v", store.data)
	}
}

func TestIncompleteKeyringSessionIsRejected(t *testing.T) {
	for key, value := range map[string]string{keyAuthToken: "auth-only", keyCT0: "ct0-only"} {
		store := newFakeStore()
		store.data[key] = value
		cm := newConfigManagerAt(t.TempDir(), store)
		_, err := cm.Load()
		if !errors.Is(err, ErrSessionIncomplete) {
			t.Errorf("key %s: got %v, want ErrSessionIncomplete", key, err)
		}
	}
}

func TestSaveRejectsIncompleteSession(t *testing.T) {
	store := newFakeStore()
	cm := newConfigManagerAt(t.TempDir(), store)
	for _, cfg := range []*Config{{AuthToken: "auth"}, {CT0: "ct0"}, nil} {
		if err := cm.Save(cfg); err == nil {
			t.Fatalf("Save(%+v) succeeded", cfg)
		}
	}
	if len(store.data) != 0 {
		t.Fatalf("incomplete session touched keyring: %+v", store.data)
	}
}

func TestLoadRepairsConfigPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".xeet.yaml")
	if err := os.WriteFile(path, []byte("create_tweet_qid: qid\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := newConfigManagerAt(dir, newFakeStore()).Load(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions = %o, want 0600", info.Mode().Perm())
	}
}
