package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	in := &Config{AuthToken: "tok123", CT0: "csrf456", CreateTweetQID: "qid789"}
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

	if err := cm.Save(&Config{AuthToken: "supersecret", CT0: "alsosecret", CreateTweetQID: "qid"}); err != nil {
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
