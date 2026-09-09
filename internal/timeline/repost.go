package timeline

import (
	"context"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"
)

type repostMsg struct {
	id       string
	reposted bool
	err      error
}

func (m *Model) toggleSelectedRepost() tea.Cmd {
	post, ok := m.currentPost()
	if !ok || m.reposting[post.ID] {
		return nil
	}
	if m.reposting == nil {
		m.reposting = map[string]bool{}
	}
	m.reposting[post.ID] = true
	wanted := !post.Reposted
	m.applyRepost(post.ID, wanted)
	m.syncViewport()
	parent := m.requestContext()
	return func() tea.Msg {
		result := repostMsg{id: post.ID, reposted: wanted}
		mgr, err := config.NewConfigManager()
		if err != nil {
			result.err = err
			return result
		}
		cfg, err := mgr.Load()
		if err != nil {
			result.err = err
			return result
		}
		ctx, cancel := context.WithTimeout(parent, 40*time.Second)
		defer cancel()
		client := api.NewWebClient(cfg)
		result.err = client.SetTweetReposted(ctx, post.ID, wanted)
		if client.ApplyRefreshedQueryIDs(cfg) {
			_ = mgr.Save(cfg)
		}
		return result
	}
}

func (m *Model) applyRepost(id string, reposted bool) {
	apply := func(post *api.TimelinePost) {
		if post.ID != id || post.Reposted == reposted {
			return
		}
		post.Reposted = reposted
		if reposted {
			post.RepostCount++
		} else if post.RepostCount > 0 {
			post.RepostCount--
		}
	}
	for i := range m.profile.posts {
		apply(&m.profile.posts[i])
	}
	for j := range m.profileStack {
		if state := m.profileStack[j].thread; state != nil {
			for i := range state.posts {
				apply(&state.posts[i].TimelinePost)
			}
		}
		for i := range m.profileStack[j].profile.posts {
			apply(&m.profileStack[j].profile.posts[i])
		}
	}
	for i := range m.posts {
		apply(&m.posts[i])
	}
	for _, cached := range m.feedCache {
		for i := range cached.posts {
			apply(&cached.posts[i])
		}
	}
	for i := range m.threadPosts {
		apply(&m.threadPosts[i].TimelinePost)
	}
	for i := range m.notifications {
		apply(&m.notifications[i].Post)
	}
	if m.notificationThread != nil {
		for i := range m.notificationThread.posts {
			apply(&m.notificationThread.posts[i].TimelinePost)
		}
	}
}

func (m Model) primaryActions() string {
	like, repost := "like", "repost"
	if post, ok := m.currentPost(); ok {
		if post.Liked {
			like = "unlike"
		}
		if post.Reposted {
			repost = "undo rt"
		}
		if m.reposting[post.ID] {
			repost = "saving…"
		}
	}
	if m.contentWidth() < 48 {
		return "l ♥  t rt  r reply  c post  ?"
	}
	return ansi.Truncate(fmt.Sprintf("l %s · t %s · r reply · u profile · c post · ? help", like, repost), m.contentWidth(), "…")
}
