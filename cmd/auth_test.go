package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"
)

type fakeAuthConfigStore struct {
	loaded           *config.Config
	accounts         map[string]config.Config
	events           *[]string
	saved            *config.Config
	saveAccountCalls int
}

func (f *fakeAuthConfigStore) Load() (*config.Config, error) {
	cfg := *f.loaded
	cfg.Columns = append([]string(nil), f.loaded.Columns...)
	return &cfg, nil
}

func (f *fakeAuthConfigStore) RecordViewer(userID, handle string) error {
	*f.events = append(*f.events, "record-viewer")
	return nil
}

func (f *fakeAuthConfigStore) SaveAccount(cfg *config.Config) error {
	*f.events = append(*f.events, "save-account")
	f.saveAccountCalls++
	saved := *cfg
	saved.Columns = append([]string(nil), cfg.Columns...)
	f.saved = &saved
	if f.accounts == nil {
		f.accounts = make(map[string]config.Config)
	}
	f.accounts[cfg.UserID] = saved
	return nil
}

func (f *fakeAuthConfigStore) SaveQueryIDs(cfg *config.Config) error {
	*f.events = append(*f.events, "save-query-ids")
	return nil
}

type fakeAuthSessionClient struct {
	events        *[]string
	verifyErr     error
	viewer        *api.Account
	viewerErr     error
	fetchAttempts int
}

func (f *fakeAuthSessionClient) Verify(context.Context) error {
	*f.events = append(*f.events, "verify")
	return f.verifyErr
}

func (f *fakeAuthSessionClient) FetchViewer(context.Context) (*api.Account, error) {
	*f.events = append(*f.events, "fetch-viewer")
	f.fetchAttempts++
	return f.viewer, f.viewerErr
}

func (f *fakeAuthSessionClient) ApplyRefreshedQueryIDs(*config.Config) bool { return false }

func TestVerifyAndSaveKeysByViewerUserIDAndDoesNotInheritSessionMetadata(t *testing.T) {
	events := []string{}
	loaded := &config.Config{
		AuthToken: "old-auth", CT0: "old-ct0", UserID: "1560376068", Handle: "first",
		HomeTimelineQID: "home-qid", ViewerQID: "viewer-qid", Theme: "dracula",
		Columns:         []string{"foryou", "following"},
		SessionBrowser:  "Firefox",
		SessionProfile:  "old-profile",
		SessionDomain:   "twitter.com",
		SessionExpires:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		SessionImported: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	store := &fakeAuthConfigStore{loaded: loaded, events: &events}
	client := &fakeAuthSessionClient{
		events: &events,
		viewer: &api.Account{ID: "42", Handle: "second"},
	}
	imported := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	result := &api.LoginResult{
		AuthToken: "new-auth", CT0: "new-ct0", Profile: "Profile 8",
		CookieDomain: "x.com", ExpiresAt: imported.Add(24 * time.Hour),
	}

	conn, err := verifyAndSaveWith(context.Background(), result, "Chrome", store,
		func(cfg *config.Config) authSessionClient { return client },
		func() time.Time { return imported })
	if err != nil {
		t.Fatal(err)
	}
	if conn.handle != "second" || store.saved == nil {
		t.Fatalf("connection=%+v saved=%+v", conn, store.saved)
	}
	if store.saved.UserID != "42" || store.saved.Handle != "second" {
		t.Fatalf("saved identity = %q @%s, want viewer identity 42 @second", store.saved.UserID, store.saved.Handle)
	}
	if store.saved.AuthToken != "new-auth" || store.saved.CT0 != "new-ct0" ||
		store.saved.SessionBrowser != "Chrome" || store.saved.SessionProfile != "Profile 8" ||
		store.saved.SessionDomain != "x.com" || !store.saved.SessionImported.Equal(imported) {
		t.Fatalf("saved session inherited active-account metadata: %+v", store.saved)
	}
	if store.saved.HomeTimelineQID != "home-qid" || store.saved.ViewerQID != "viewer-qid" ||
		store.saved.Theme != "dracula" || len(store.saved.Columns) != 2 {
		t.Fatalf("global settings were not retained: %+v", store.saved)
	}
	wantOrder := "verify,fetch-viewer,record-viewer,save-account,save-query-ids"
	if got := strings.Join(events, ","); got != wantOrder {
		t.Fatalf("auth order = %s, want %s", got, wantOrder)
	}
}

func TestReauthOfKnownAccountUpdatesInPlaceNotNewSlot(t *testing.T) {
	events := []string{}
	old := config.Config{UserID: "42", AuthToken: "old-auth", CT0: "old-ct0"}
	store := &fakeAuthConfigStore{
		loaded:   &old,
		accounts: map[string]config.Config{"42": old},
		events:   &events,
	}
	client := &fakeAuthSessionClient{
		events: &events,
		viewer: &api.Account{ID: "42", Handle: "renamed"},
	}
	result := &api.LoginResult{AuthToken: "new-auth", CT0: "new-ct0", Profile: "Default"}

	if _, err := verifyAndSaveWith(context.Background(), result, "Chrome", store,
		func(cfg *config.Config) authSessionClient { return client }, time.Now); err != nil {
		t.Fatal(err)
	}
	if len(store.accounts) != 1 {
		t.Fatalf("reauth created %d account slots, want 1", len(store.accounts))
	}
	updated := store.accounts["42"]
	if updated.AuthToken != "new-auth" || updated.Handle != "renamed" {
		t.Fatalf("known account was not updated in place: %+v", updated)
	}
}

func TestVerifyAndSaveFailsClosedWhenViewerUnidentifiable(t *testing.T) {
	events := []string{}
	store := &fakeAuthConfigStore{loaded: &config.Config{}, events: &events}
	client := &fakeAuthSessionClient{
		events:    &events,
		viewerErr: errors.New("identity endpoint unavailable"),
	}
	result := &api.LoginResult{AuthToken: "auth", CT0: "ct0"}

	_, err := verifyAndSaveWith(context.Background(), result, "Chrome", store,
		func(cfg *config.Config) authSessionClient { return client }, time.Now)
	if err == nil || !strings.Contains(err.Error(), "verified but xeet could not identify the account; try again") {
		t.Fatalf("error = %v, want fail-closed identification guidance", err)
	}
	if client.fetchAttempts != 2 {
		t.Fatalf("FetchViewer attempts = %d, want one retry", client.fetchAttempts)
	}
	if store.saveAccountCalls != 0 {
		t.Fatalf("SaveAccount called %d times without a viewer id", store.saveAccountCalls)
	}
}
