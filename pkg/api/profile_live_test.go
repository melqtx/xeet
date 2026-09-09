package api

import (
	"context"
	"github.com/melqtx/xeet/pkg/config"
	"os"
	"testing"
	"time"
)

// Opt-in, read-only integration check using the existing local session.
func TestProfileLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_PROFILE") != "1" {
		t.Skip("set XEET_LIVE_PROFILE=1 to read your account profile")
	}
	mgr, err := config.NewConfigManager()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	c := NewWebClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	viewer, err := c.FetchViewer(ctx)
	if err != nil {
		t.Fatalf("viewer: %v", err)
	}
	handle := os.Getenv("XEET_LIVE_PROFILE_HANDLE")
	if handle == "" {
		handle = viewer.Handle
	}
	p, err := c.FetchProfile(ctx, handle)
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if handle == viewer.Handle && p.ID != viewer.ID {
		t.Fatal("profile identity mismatch")
	}
	if !p.HasPosts || !p.HasFollowers || !p.HasFollowing {
		t.Fatal("profile omitted public counts")
	}
	t.Logf("posts=%d followers=%d following=%d bio_present=%t website_present=%t", p.Posts, p.Followers, p.Following, p.Bio != "", p.Website != "")
	page, err := c.FetchProfilePosts(ctx, p.ID, "", 5)
	if err != nil {
		t.Fatalf("posts: %v", err)
	}
	t.Logf("profile loaded; %d posts returned", len(page.Posts))
}
