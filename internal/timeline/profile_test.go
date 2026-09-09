package timeline

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/melqtx/xeet/pkg/api"
	"strings"
	"testing"
)

func TestProfileNavigationRestoresFeedAndRejectsLatePage(t *testing.T) {
	m := NewWithImageMode("off")
	m.width = 80
	m.height = 24
	m.loading = false
	m.posts = []api.TimelinePost{{ID: "one", Handle: "alice"}, {ID: "two", Handle: "bob"}}
	m.selected = 1
	m.expanded = true
	m.resize()
	offset := m.viewport.YOffset
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = next.(Model)
	if m.mode != modeProfile || m.profile.info.Handle != "bob" {
		t.Fatal("did not open author")
	}
	seq := m.profileSeq
	next, _ = m.leaveProfile()
	m = next.(Model)
	if m.mode != modeFeed || m.selected != 1 || !m.expanded || m.viewport.YOffset != offset {
		t.Fatal("feed position lost")
	}
	next, _ = m.applyProfilePage(profileMsg{seq: seq, info: &api.Profile{Account: api.Account{Handle: "late"}}})
	m = next.(Model)
	if m.profile.info.Handle == "late" {
		t.Fatal("late response accepted")
	}
}

func TestProfilePagesDeduplicateAndThreadReturns(t *testing.T) {
	m := NewWithImageMode("off")
	m.mode = modeProfile
	m.width = 80
	m.height = 24
	m.profileSeq = 7
	m.profile.posts = []api.TimelinePost{{ID: "1", Handle: "alice"}, {ID: "2", Handle: "alice"}}
	m.selected = 1
	m.resize()
	next, _ := m.applyProfilePage(profileMsg{seq: 7, more: true, page: &api.TimelinePage{Posts: []api.TimelinePost{{ID: "2"}, {ID: "3"}}, Cursor: "next"}})
	m = next.(Model)
	if len(m.profile.posts) != 3 || m.selected != 1 || m.profile.cursor != "next" {
		t.Fatal("pagination lost selection or duplicated posts")
	}
	next, _ = m.updateProfile(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.mode != modeThread || m.threadReturn != modeProfile {
		t.Fatal("profile thread not opened")
	}
	next, _ = m.leaveThread()
	m = next.(Model)
	if m.mode != modeProfile || m.selected != 1 {
		t.Fatal("thread did not return to profile")
	}
}

func TestProfileHeaderFitsAndPostOffsetsMatch(t *testing.T) {
	for _, width := range []int{34, 44, 52, 60, 80, 120} {
		m := NewWithImageMode("off")
		m.mode = modeProfile
		m.width = width
		m.height = 24
		m.profile.info = api.Profile{Account: api.Account{ID: "1", Name: "Alice", Handle: "alice"}, Bio: strings.Repeat("old forums are nice ", 8), Website: "https://example.com/" + strings.Repeat("long", 20), Followers: 1200}
		m.profile.posts = []api.TimelinePost{{ID: "post", Text: "a unique post", Handle: "alice"}}
		content, starts, _ := m.renderProfileContent()
		if lipgloss.Width(content) > m.contentWidth() {
			t.Fatalf("overflow at %d", width)
		}
		lines := strings.Split(ansi.Strip(content), "\n")
		if len(starts) != 1 || !strings.Contains(lines[starts[0]], "@alice") {
			t.Fatal("post offset did not account for header")
		}
		if !strings.Contains(ansi.Strip(content), "Alice\n@alice") || !strings.Contains(content, "old forums") {
			t.Fatal("missing profile identity or bio")
		}
	}
}

func TestNestedProfileRestoresParentConversation(t *testing.T) {
	m := NewWithImageMode("off")
	m.width = 80
	m.height = 24
	m.mode = modeProfile
	m.profile.info.Handle = "alice"
	m.profile.posts = []api.TimelinePost{{ID: "a", Handle: "alice"}, {ID: "b", Handle: "alice"}}
	m.selected = 1
	m.resize()
	next, _ := m.updateProfile(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	root := m.threadRootID
	next, _ = m.beginProfile(api.TimelinePost{Handle: "bob"})
	m = next.(Model)
	m.profileSelected = 0
	next, _ = m.leaveProfile()
	m = next.(Model)
	if m.mode != modeThread || m.threadRootID != root {
		t.Fatal("parent conversation lost")
	}
	next, _ = m.leaveThread()
	m = next.(Model)
	if m.mode != modeProfile || m.profile.info.Handle != "alice" || m.selected != 1 {
		t.Fatal("parent profile position lost")
	}
}

func TestProfileHeaderSurvivesPreviewRelayout(t *testing.T) {
	m := NewWithImageMode("off")
	m.width = 80
	m.height = 15
	m.mode = modeProfile
	m.profile.info.Bio = strings.Repeat("bio\n", 12)
	m.profile.posts = []api.TimelinePost{{ID: "a", Handle: "alice", Text: "hello"}}
	m.resize()
	m.ensureSelectedVisible()
	if m.viewport.YOffset != 0 {
		t.Fatal("profile header scrolled away before navigation")
	}
}

func TestProfileUnknownCountsAreNotRenderedAsZeros(t *testing.T) {
	m := NewWithImageMode("off")
	m.width = 80
	m.profile.info = api.Profile{Account: api.Account{ID: "42", Handle: "alice"}}
	content, _, _ := m.renderProfileContent()
	if !strings.Contains(ansi.Strip(content), "—") || strings.Contains(ansi.Strip(content), "0") {
		t.Fatal("missing counts should be unavailable")
	}
	m.profile.info.HasPosts = true
	m.profile.info.HasFollowers = true
	m.profile.info.HasFollowing = true
	content, _, _ = m.renderProfileContent()
	if !strings.Contains(ansi.Strip(content), "0") || strings.Contains(ansi.Strip(content), "—") {
		t.Fatal("genuine zero counts should remain zero")
	}
}

func TestProfileDetailsAndExactCounts(t *testing.T) {
	m := NewWithImageMode("off")
	m.width = 80
	m.profile.info = api.Profile{
		Account:  api.Account{ID: "42", Name: "Alice Chen", Handle: "alice", Verified: true},
		Bio:      "Making small things for the web.\nUsually with a cup of tea.",
		Location: "London", Website: "https://alice.example", Joined: "Fri Sep 29 20:18:58 +0000 2023",
		Followers: 12345, Following: 128, Posts: 2048, HasFollowers: true, HasFollowing: true, HasPosts: true,
		FollowsYou: true, YouFollow: true,
	}
	content, _, _ := m.renderProfileContent()
	plain := ansi.Strip(content)
	for _, want := range []string{"verified · follows you · following", "12,345", "2,048", "based in  London", "website   https://alice.example", "joined    September 2023", "Usually with a cup of tea."} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in:\n%s", want, plain)
		}
	}
	t.Log("\n" + plain)
}
