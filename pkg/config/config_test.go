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
	"reflect"
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
		UserID: "42", Handle: "alice",
		HomeTimelineQID: "home123", BookmarksQID: "bookmarks123", SearchTimelineQID: "search123",
		ListLatestTweetsTimelineQID: "listtimeline123", ListsManagementPageTimelineQID: "listsmanagement123",
		FavoriteTweetQID: "like123", UnfavoriteTweetQID: "unlike123", ViewerQID: "viewer123",
		TweetDetailQID: "detail123",
		Columns:        []string{"foryou", "bookmarks"},
		SessionBrowser: "Firefox", SessionProfile: "default-release", SessionDomain: "x.com",
		SessionExpires:  time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC),
		SessionImported: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC),
	}
	if err := cm.Save(in); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".xeet.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"42\":") {
		t.Fatalf("numeric user id was not quoted as a YAML string:\n%s", data)
	}

	out, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", out, in)
	}
}

func TestTokensNeverTouchDisk(t *testing.T) {
	dir := t.TempDir()
	cm := newConfigManagerAt(dir, newFakeStore())

	if err := cm.Save(&Config{
		AuthToken: "supersecret", CT0: "alsosecret", CreateTweetQID: "qid",
		UserID: "42", Handle: "alice",
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

func TestSaveWithoutSessionPersistsBookmarksQueryID(t *testing.T) {
	dir := t.TempDir()
	cm := newConfigManagerAt(dir, newFakeStore())

	if err := cm.Save(&Config{BookmarksQID: "bookmarks123"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BookmarksQID != "bookmarks123" {
		t.Fatalf("BookmarksQID = %q, want bookmarks123", cfg.BookmarksQID)
	}
}

func TestSaveWithoutSessionPersistsSearchTimelineQueryID(t *testing.T) {
	dir := t.TempDir()
	cm := newConfigManagerAt(dir, newFakeStore())

	if err := cm.Save(&Config{SearchTimelineQID: "search123"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SearchTimelineQID != "search123" {
		t.Fatalf("SearchTimelineQID = %q, want search123", cfg.SearchTimelineQID)
	}
}

func TestSaveWithoutSessionPersistsListsQueryIDs(t *testing.T) {
	dir := t.TempDir()
	cm := newConfigManagerAt(dir, newFakeStore())

	if err := cm.Save(&Config{
		ListLatestTweetsTimelineQID:    "listtimeline123",
		ListsManagementPageTimelineQID: "listsmanagement123",
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListLatestTweetsTimelineQID != "listtimeline123" ||
		cfg.ListsManagementPageTimelineQID != "listsmanagement123" {
		t.Fatalf("lists query ids were not persisted: %+v", cfg)
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
	if !reflect.DeepEqual(cfg, &Config{}) {
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
	store.data[keyLegacyAuthToken] = "auth"
	store.data[keyLegacyCT0] = "ct0"
	cfg, err := newConfigManagerAt(dir, store).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthToken != "auth" || cfg.UserID != legacyAccountID ||
		cfg.SessionBrowser != "Chrome" || !cfg.SessionExpires.IsZero() {
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
	if store.data[authTokenKey(legacyAccountID)] != "oldtoken" ||
		store.data[ct0Key(legacyAccountID)] != "oldct0" {
		t.Fatalf("keyring after migration: %+v", store.data)
	}
	if _, ok := store.data[keyLegacyAuthToken]; ok {
		t.Fatalf("legacy auth key survived migration: %+v", store.data)
	}
	if _, ok := store.data[keyLegacyCT0]; ok {
		t.Fatalf("legacy ct0 key survived migration: %+v", store.data)
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

	if err := cm.Save(&Config{
		UserID: "42", AuthToken: "tok", CT0: "ct", CreateTweetQID: "qid",
	}); err != nil {
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
	base.data[authTokenKey("42")] = "old-auth"
	base.data[ct0Key("42")] = "old-ct0"
	store := &failingStore{fakeStore: base, failKey: ct0Key("42"), failValue: "new-ct0"}
	cm := newConfigManagerAt(t.TempDir(), store)

	err := cm.Save(&Config{
		UserID: "42", AuthToken: "new-auth", CT0: "new-ct0", CreateTweetQID: "qid",
	})
	if err == nil {
		t.Fatal("expected injected keyring error")
	}
	if base.data[authTokenKey("42")] != "old-auth" || base.data[ct0Key("42")] != "old-ct0" {
		t.Fatalf("partial save was not rolled back: %+v", base.data)
	}
}

func TestSaveRollsBackWhenConfigWriteFails(t *testing.T) {
	dir := t.TempDir()
	store := newFakeStore()
	store.data[authTokenKey("42")] = "old-auth"
	store.data[ct0Key("42")] = "old-ct0"
	cm := newConfigManagerAt(dir, store)

	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, cm.configPath); err != nil {
		t.Fatal(err)
	}

	err := cm.Save(&Config{
		UserID: "42", AuthToken: "new-auth", CT0: "new-ct0", CreateTweetQID: "qid",
	})
	if err == nil {
		t.Fatal("expected config write error")
	}
	if store.data[authTokenKey("42")] != "old-auth" || store.data[ct0Key("42")] != "old-ct0" {
		t.Fatalf("keyring was not rolled back: %+v", store.data)
	}
}

func TestIncompleteKeyringSessionIsRejected(t *testing.T) {
	for key, value := range map[string]string{
		authTokenKey("42"): "auth-only",
		ct0Key("42"):       "ct0-only",
	} {
		dir := t.TempDir()
		store := newFakeStore()
		store.data[key] = value
		if err := os.WriteFile(filepath.Join(dir, ".xeet.yaml"), []byte(
			"version: 2\nactive: \"42\"\naccounts:\n  \"42\":\n    handle: alice\n",
		), 0600); err != nil {
			t.Fatal(err)
		}
		cm := newConfigManagerAt(dir, store)
		_, err := cm.Load()
		if !errors.Is(err, ErrSessionIncomplete) {
			t.Errorf("key %s: got %v, want ErrSessionIncomplete", key, err)
		}
	}
}

func TestSaveRejectsIncompleteSession(t *testing.T) {
	store := newFakeStore()
	cm := newConfigManagerAt(t.TempDir(), store)
	for _, cfg := range []*Config{
		{UserID: "42", AuthToken: "auth"},
		{UserID: "42", CT0: "ct0"},
		nil,
	} {
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

// countingStore records whether anything reached the keyring at all.
type countingStore struct {
	*fakeStore
	gets int
}

func (c *countingStore) Get(key string) (string, error) {
	c.gets++
	return c.fakeStore.Get(key)
}

func TestThemeReadsTheFileWithoutTouchingTheKeyring(t *testing.T) {
	dir := t.TempDir()
	store := &countingStore{fakeStore: newFakeStore()}
	cm := newConfigManagerAt(dir, store)

	if err := cm.Save(&Config{Theme: "nord"}); err != nil {
		t.Fatal(err)
	}
	store.gets = 0

	name, err := cm.Theme()
	if err != nil {
		t.Fatal(err)
	}
	if name != "nord" {
		t.Fatalf("Theme() = %q, want nord", name)
	}
	if store.gets != 0 {
		t.Fatalf("Theme() made %d keyring reads; drawing in the right colors must not prompt for secrets", store.gets)
	}
}

func TestThemeIsEmptyWithoutAConfigFile(t *testing.T) {
	name, err := newConfigManagerAt(t.TempDir(), newFakeStore()).Theme()
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Fatalf("Theme() = %q, want empty", name)
	}
}

func TestColumnsReadTheFileWithoutTouchingTheKeyring(t *testing.T) {
	dir := t.TempDir()
	store := &countingStore{fakeStore: newFakeStore()}
	cm := newConfigManagerAt(dir, store)
	if err := cm.Save(&Config{
		UserID:    "42",
		AuthToken: "auth",
		CT0:       "ct0",
		Columns:   []string{"foryou", "bookmarks"},
	}); err != nil {
		t.Fatal(err)
	}
	store.gets = 0

	columns, err := cm.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(columns, []string{"foryou", "bookmarks"}) {
		t.Fatalf("Columns() = %v", columns)
	}
	if store.gets != 0 {
		t.Fatalf("Columns() made %d keyring reads", store.gets)
	}
}

func TestRefreshIntervalRoundTripsWithoutTouchingTheKeyring(t *testing.T) {
	dir := t.TempDir()
	store := &countingStore{fakeStore: newFakeStore()}
	cm := newConfigManagerAt(dir, store)
	if err := cm.Save(&Config{RefreshInterval: "90s"}); err != nil {
		t.Fatal(err)
	}
	store.gets = 0

	interval, err := cm.RefreshInterval()
	if err != nil {
		t.Fatal(err)
	}
	if interval != 90*time.Second {
		t.Fatalf("RefreshInterval() = %v, want 90s", interval)
	}
	if store.gets != 0 {
		t.Fatalf("RefreshInterval() made %d keyring reads; resolving a display setting must not prompt for secrets", store.gets)
	}
}

func TestRefreshIntervalDefaultsToOff(t *testing.T) {
	interval, err := newConfigManagerAt(t.TempDir(), newFakeStore()).RefreshInterval()
	if err != nil {
		t.Fatal(err)
	}
	if interval != 0 {
		t.Fatalf("RefreshInterval() = %v, want 0 so polling stays opt-in", interval)
	}
}

func TestRefreshIntervalRejectsGarbage(t *testing.T) {
	cm := newConfigManagerAt(t.TempDir(), newFakeStore())
	if err := cm.Save(&Config{RefreshInterval: "abc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cm.RefreshInterval(); err == nil {
		t.Fatal("RefreshInterval() accepted an unparseable duration")
	}
}

func TestSaveColumnsPatchesOnlyTheLayout(t *testing.T) {
	dir := t.TempDir()
	store := &countingStore{fakeStore: newFakeStore()}
	cm := newConfigManagerAt(dir, store)
	if err := cm.Save(&Config{
		UserID:         "42",
		AuthToken:      "auth",
		CT0:            "ct0",
		CreateTweetQID: "qid",
		Theme:          "nord",
		Columns:        []string{"foryou"},
	}); err != nil {
		t.Fatal(err)
	}
	store.gets = 0

	if err := cm.SaveColumns([]string{"following", "search:golang"}); err != nil {
		t.Fatal(err)
	}
	if store.gets != 0 {
		t.Fatalf("SaveColumns made %d keyring reads", store.gets)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".xeet.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"create_tweet_qid: qid", "theme: nord", "following", "search:golang"} {
		if !strings.Contains(body, want) {
			t.Fatalf("SaveColumns dropped %q:\n%s", want, body)
		}
	}
}

type operationStore struct {
	*fakeStore
	operations           []string
	failDelete           string
	configPath           string
	completeBeforeDelete bool
}

func (s *operationStore) Get(key string) (string, error) {
	s.operations = append(s.operations, "get "+key)
	return s.fakeStore.Get(key)
}

func (s *operationStore) Set(key, value string) error {
	s.operations = append(s.operations, "set "+key)
	return s.fakeStore.Set(key, value)
}

func (s *operationStore) Delete(key string) error {
	s.operations = append(s.operations, "delete "+key)
	if key == s.failDelete {
		body, err := os.ReadFile(s.configPath)
		if err == nil &&
			strings.Contains(string(body), "version: 2") &&
			strings.Contains(string(body), "active:") &&
			s.data[authTokenKey(accountIDForDelete(key))] != "" &&
			s.data[ct0Key(accountIDForDelete(key))] != "" {
			s.completeBeforeDelete = true
		}
		return errors.New("injected delete failure")
	}
	return s.fakeStore.Delete(key)
}

func accountIDForDelete(key string) string {
	if key == keyLegacyAuthToken {
		return legacyAccountID
	}
	return strings.TrimPrefix(key, "auth_token:")
}

func TestLoadMigratesV1KeyringSessionToV2UnderProvisionalKey(t *testing.T) {
	dir := t.TempDir()
	body := strings.Join([]string{
		"bookmarks_qid: bookmarks-v1",
		"search_timeline_qid: search-v1",
		"list_latest_tweets_timeline_qid: list-v1",
		"session_browser: Chrome",
		"session_profile: Default",
		"session_domain: .x.com",
		"session_expires: 2027-01-01T00:00:00Z",
		"session_imported: 2026-07-27T00:00:00Z",
		"columns:",
		"  - foryou",
		"  - following",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, ".xeet.yaml"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	store.data[keyLegacyAuthToken] = "legacy-auth"
	store.data[keyLegacyCT0] = "legacy-ct0"

	cfg, err := newConfigManagerAt(dir, store).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserID != legacyAccountID || cfg.AuthToken != "legacy-auth" || cfg.CT0 != "legacy-ct0" {
		t.Fatalf("migrated active config = %+v", cfg)
	}
	if cfg.BookmarksQID != "bookmarks-v1" || cfg.SearchTimelineQID != "search-v1" ||
		cfg.ListLatestTweetsTimelineQID != "list-v1" {
		t.Fatalf("migration lost query ids: %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.Columns, []string{"foryou", "following"}) {
		t.Fatalf("migration lost columns: %v", cfg.Columns)
	}
	if store.data[authTokenKey(legacyAccountID)] != "legacy-auth" ||
		store.data[ct0Key(legacyAccountID)] != "legacy-ct0" {
		t.Fatalf("provisional keyring pair = %+v", store.data)
	}
	if _, ok := store.data[keyLegacyAuthToken]; ok {
		t.Fatalf("unsuffixed auth token survived: %+v", store.data)
	}
	if _, ok := store.data[keyLegacyCT0]; ok {
		t.Fatalf("unsuffixed ct0 survived: %+v", store.data)
	}
	written, err := os.ReadFile(filepath.Join(dir, ".xeet.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"version: 2", "active: legacy", "accounts:", "legacy:",
		"bookmarks_qid: bookmarks-v1", "search_timeline_qid: search-v1",
		"list_latest_tweets_timeline_qid: list-v1", "columns:",
	} {
		if !strings.Contains(string(written), want) {
			t.Fatalf("migrated file missing %q:\n%s", want, written)
		}
	}
}

func TestMigrationCrashBeforeLegacyDeleteIsRecoverable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".xeet.yaml"), []byte(
		"bookmarks_qid: preserved\nsession_browser: Chrome\n",
	), 0600); err != nil {
		t.Fatal(err)
	}
	base := newFakeStore()
	base.data[keyLegacyAuthToken] = "old-auth"
	base.data[keyLegacyCT0] = "old-ct0"
	store := &operationStore{
		fakeStore:  base,
		failDelete: keyLegacyAuthToken,
		configPath: filepath.Join(dir, ".xeet.yaml"),
	}
	cm := newConfigManagerAt(dir, store)

	if _, err := cm.Load(); err == nil {
		t.Fatal("migration unexpectedly survived the injected legacy-delete failure")
	}
	if !store.completeBeforeDelete {
		t.Fatalf("legacy delete started before the v2 file and new pair were complete: %v", store.operations)
	}
	assertOperationOrder(t, store.operations,
		"set "+authTokenKey(legacyAccountID),
		"set "+ct0Key(legacyAccountID),
		"delete "+keyLegacyAuthToken,
	)

	store.failDelete = ""
	cfg, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserID != legacyAccountID || cfg.AuthToken != "old-auth" ||
		cfg.CT0 != "old-ct0" || cfg.BookmarksQID != "preserved" {
		t.Fatalf("recovered config = %+v", cfg)
	}
}

func TestRekeyAccountMovesEntryAndKeyringPairDeleteLast(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".xeet.yaml"), []byte(
		"session_imported: 2026-07-27T00:00:00Z\n",
	), 0600); err != nil {
		t.Fatal(err)
	}
	base := newFakeStore()
	base.data[keyLegacyAuthToken] = "legacy-auth"
	base.data[keyLegacyCT0] = "legacy-ct0"
	store := &operationStore{fakeStore: base, configPath: filepath.Join(dir, ".xeet.yaml")}
	cm := newConfigManagerAt(dir, store)
	if _, err := cm.Load(); err != nil {
		t.Fatal(err)
	}
	store.operations = nil
	store.failDelete = authTokenKey(legacyAccountID)

	if err := cm.RekeyAccount(legacyAccountID, "42"); err == nil {
		t.Fatal("rekey unexpectedly survived the injected old-key delete failure")
	}
	if !store.completeBeforeDelete {
		t.Fatalf("old key deletion started before the new account was durable: %v", store.operations)
	}
	assertOperationOrder(t, store.operations,
		"set "+authTokenKey("42"),
		"set "+ct0Key("42"),
		"delete "+authTokenKey(legacyAccountID),
	)
	store.failDelete = ""
	cfg, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserID != "42" || cfg.AuthToken != "legacy-auth" || cfg.CT0 != "legacy-ct0" {
		t.Fatalf("new account did not win after interrupted cleanup: %+v", cfg)
	}
}

func TestRekeyIntoExistingUserIDMergesKeepingNewerSession(t *testing.T) {
	dir := t.TempDir()
	cm := newConfigManagerAt(dir, newFakeStore())
	older := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	if err := cm.SaveAccount(&Config{
		UserID: "legacy", AuthToken: "old-auth", CT0: "old-ct0",
		Handle: "old", SessionImported: older,
	}); err != nil {
		t.Fatal(err)
	}
	if err := cm.SaveAccount(&Config{
		UserID: "42", AuthToken: "new-auth", CT0: "new-ct0",
		Handle: "alice", SessionImported: newer,
	}); err != nil {
		t.Fatal(err)
	}
	if err := cm.SetActive("legacy"); err != nil {
		t.Fatal(err)
	}

	if err := cm.RekeyAccount("legacy", "42"); err != nil {
		t.Fatal(err)
	}
	cfg, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserID != "42" || cfg.AuthToken != "new-auth" || cfg.CT0 != "new-ct0" ||
		cfg.Handle != "alice" || !cfg.SessionImported.Equal(newer) {
		t.Fatalf("merge did not keep the newer destination session: %+v", cfg)
	}
	accounts, err := cm.Accounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].UserID != "42" {
		t.Fatalf("merged accounts = %+v", accounts)
	}
}

func TestSaveAccountRejectsEmptyUserID(t *testing.T) {
	store := newFakeStore()
	cm := newConfigManagerAt(t.TempDir(), store)
	err := cm.SaveAccount(&Config{AuthToken: "auth", CT0: "ct0"})
	if err == nil || !strings.Contains(err.Error(), "user id") {
		t.Fatalf("SaveAccount error = %v", err)
	}
	if len(store.data) != 0 {
		t.Fatalf("rejected account touched keyring: %+v", store.data)
	}
}

func TestSaveQueryIDsPatchesOnlyQIDFields(t *testing.T) {
	dir := t.TempDir()
	cm := newConfigManagerAt(dir, newFakeStore())
	if err := cm.Save(&Config{
		CreateTweetQID: "old-create",
		Theme:          "nord",
		Columns:        []string{"foryou", "following"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := cm.SaveAccount(&Config{
		UserID: "42", AuthToken: "auth-42", CT0: "ct0-42", Handle: "alice",
	}); err != nil {
		t.Fatal(err)
	}
	stale, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := cm.SaveAccount(&Config{
		UserID: "84", AuthToken: "auth-84", CT0: "ct0-84", Handle: "bob",
	}); err != nil {
		t.Fatal(err)
	}
	stale.CreateTweetQID = "fresh-create"
	stale.ViewerQID = "fresh-viewer"
	if err := cm.SaveQueryIDs(stale); err != nil {
		t.Fatal(err)
	}

	accounts, err := cm.Accounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("SaveQueryIDs clobbered accounts: %+v", accounts)
	}
	active, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if active.UserID != "84" || active.CreateTweetQID != "fresh-create" ||
		active.ViewerQID != "fresh-viewer" || active.Theme != "nord" ||
		!reflect.DeepEqual(active.Columns, []string{"foryou", "following"}) {
		t.Fatalf("SaveQueryIDs changed non-QID state: %+v", active)
	}
}

func TestSaveRefusesFileFromNewerVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".xeet.yaml")
	original := "version: 3\nactive: \"42\"\naccounts:\n  \"42\": {}\nfuture_key: keep-me\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	cm := newConfigManagerAt(dir, store)
	err := cm.Save(&Config{
		UserID: "42", AuthToken: "auth", CT0: "ct0", CreateTweetQID: "replacement",
	})
	if err == nil || !strings.Contains(err.Error(), "config written by a newer xeet") {
		t.Fatalf("Save error = %v", err)
	}
	written, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(written) != original || len(store.data) != 0 {
		t.Fatalf("newer config was modified:\n%s\nkeyring=%+v", written, store.data)
	}
}

func TestEraseAccountRemovesOnlyThatAccountAndPromotesActive(t *testing.T) {
	dir := t.TempDir()
	store := newFakeStore()
	cm := newConfigManagerAt(dir, store)
	if err := cm.Save(&Config{
		CreateTweetQID: "qid", Theme: "nord", Columns: []string{"foryou", "bookmarks"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []*Config{
		{UserID: "42", AuthToken: "auth-42", CT0: "ct0-42", Handle: "alice"},
		{UserID: "84", AuthToken: "auth-84", CT0: "ct0-84", Handle: "bob"},
	} {
		if err := cm.SaveAccount(cfg); err != nil {
			t.Fatal(err)
		}
	}
	if err := cm.SetActive("42"); err != nil {
		t.Fatal(err)
	}
	if err := cm.EraseAccount("42"); err != nil {
		t.Fatal(err)
	}
	cfg, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserID != "84" || cfg.AuthToken != "auth-84" || cfg.CreateTweetQID != "qid" ||
		cfg.Theme != "nord" || !reflect.DeepEqual(cfg.Columns, []string{"foryou", "bookmarks"}) {
		t.Fatalf("promoted config = %+v", cfg)
	}
	if _, ok := store.data[authTokenKey("42")]; ok {
		t.Fatalf("removed account auth token survived: %+v", store.data)
	}
	if _, ok := store.data[ct0Key("42")]; ok {
		t.Fatalf("removed account ct0 survived: %+v", store.data)
	}
	if store.data[authTokenKey("84")] != "auth-84" || store.data[ct0Key("84")] != "ct0-84" {
		t.Fatalf("other account secrets changed: %+v", store.data)
	}
}

func TestLoadAccountReturnsRequestedSessionNotActive(t *testing.T) {
	cm := newConfigManagerAt(t.TempDir(), newFakeStore())
	for _, cfg := range []*Config{
		{UserID: "42", AuthToken: "auth-42", CT0: "ct0-42", Handle: "alice"},
		{UserID: "84", AuthToken: "auth-84", CT0: "ct0-84", Handle: "bob"},
	} {
		if err := cm.SaveAccount(cfg); err != nil {
			t.Fatal(err)
		}
	}
	requested, err := cm.LoadAccount("42")
	if err != nil {
		t.Fatal(err)
	}
	active, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if requested.UserID != "42" || requested.AuthToken != "auth-42" || requested.Handle != "alice" {
		t.Fatalf("requested account = %+v", requested)
	}
	if active.UserID != "84" {
		t.Fatalf("LoadAccount changed active account: %+v", active)
	}
}

func TestAccountsListsMetadataWithoutTouchingKeyring(t *testing.T) {
	dir := t.TempDir()
	store := &countingStore{fakeStore: newFakeStore()}
	cm := newConfigManagerAt(dir, store)
	if err := cm.SaveAccount(&Config{
		UserID: "42", AuthToken: "auth", CT0: "ct0", Handle: "alice",
		SessionBrowser: "Chrome", SessionProfile: "Default",
		SessionImported: time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	store.gets = 0

	accounts, err := cm.Accounts()
	if err != nil {
		t.Fatal(err)
	}
	if store.gets != 0 {
		t.Fatalf("Accounts made %d keyring reads", store.gets)
	}
	if len(accounts) != 1 || accounts[0].UserID != "42" ||
		accounts[0].Handle != "alice" || accounts[0].SessionBrowser != "Chrome" ||
		accounts[0].SessionProfile != "Default" || !accounts[0].Active {
		t.Fatalf("accounts = %+v", accounts)
	}
}

func TestPreKeyringLegacyFileMigratesStraightToV2(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".xeet.key"), key, 0600); err != nil {
		t.Fatal(err)
	}
	encrypted := legacyEncrypt(t, "file-auth", key)
	body := "auth_token: " + encrypted + "\nct0: file-ct0\nsearch_timeline_qid: search-v1\n"
	if err := os.WriteFile(filepath.Join(dir, ".xeet.yaml"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	cfg, err := newConfigManagerAt(dir, store).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserID != legacyAccountID || cfg.AuthToken != "file-auth" ||
		cfg.CT0 != "file-ct0" || cfg.SearchTimelineQID != "search-v1" {
		t.Fatalf("pre-keyring migration = %+v", cfg)
	}
	written, err := os.ReadFile(filepath.Join(dir, ".xeet.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "version: 2") ||
		strings.Contains(string(written), encrypted) || strings.Contains(string(written), "file-ct0") {
		t.Fatalf("pre-keyring file was not scrubbed into v2:\n%s", written)
	}
}

func TestRecordViewerPromotesLegacyAndSetsHandle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".xeet.yaml"), []byte(
		"session_imported: 2026-07-27T00:00:00Z\n",
	), 0600); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	store.data[keyLegacyAuthToken] = "legacy-auth"
	store.data[keyLegacyCT0] = "legacy-ct0"
	cm := newConfigManagerAt(dir, store)
	if _, err := cm.Load(); err != nil {
		t.Fatal(err)
	}
	if err := cm.RecordViewer("42", "alice"); err != nil {
		t.Fatal(err)
	}
	cfg, err := cm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserID != "42" || cfg.Handle != "alice" || cfg.AuthToken != "legacy-auth" {
		t.Fatalf("recorded viewer = %+v", cfg)
	}
}

func assertOperationOrder(t *testing.T, operations []string, wants ...string) {
	t.Helper()
	next := 0
	for _, operation := range operations {
		if next < len(wants) && operation == wants[next] {
			next++
		}
	}
	if next != len(wants) {
		t.Fatalf("operations %v do not contain ordered sequence %v", operations, wants)
	}
}
