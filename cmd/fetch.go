package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var (
	fetchText    bool
	fetchReplies int
	fetchAccount string
)

var fetchCmd = &cobra.Command{
	Use:   "fetch <url-or-id>",
	Short: "fetch a single post as JSON, for scripts and agents",
	Long: `Fetch one post without opening the interface and print it as JSON on
stdout — the shape scripts and AI agents can consume directly. The argument is
a post URL (x.com or twitter.com) or a bare status id. Progress and errors go
to stderr, so stdout stays pipe-clean; a missing or invisible post exits
non-zero. Pass --text for a human-readable rendering instead, and --replies N
to include up to N replies in the output.`,
	Example: `  xeet fetch https://x.com/alice/status/1234567890
  xeet fetch 1234567890 | jq .text
  xeet fetch https://twitter.com/alice/status/123 --text
  xeet fetch https://x.com/alice/status/123 --replies 5`,
	Args: cobra.ExactArgs(1),
	RunE: runFetch,
}

func init() {
	fetchCmd.Flags().BoolVar(&fetchText, "text", false, "print a human-readable rendering instead of JSON")
	fetchCmd.Flags().IntVar(&fetchReplies, "replies", 0, "include up to N replies in the output")
	fetchCmd.Flags().StringVar(&fetchAccount, "account", "", "saved account to read as (handle or user id); defaults to the active one")
	rootCmd.AddCommand(fetchCmd)
}

// The JSON schema is defined here rather than marshalling api.TimelinePost
// directly: agents build on these keys, so they must not shift when the
// internal struct does.
type fetchAuthor struct {
	Name   string `json:"name"`
	Handle string `json:"handle"`
}

type fetchMedia struct {
	URL     string `json:"url"`
	Type    string `json:"type"`
	AltText string `json:"alt_text,omitempty"`
}

type fetchArticle struct {
	Title string `json:"title,omitempty"`
	Text  string `json:"text"`
}

type fetchPost struct {
	ID             string        `json:"id"`
	URL            string        `json:"url"`
	Author         fetchAuthor   `json:"author"`
	CreatedAt      string        `json:"created_at,omitempty"`
	Text           string        `json:"text"`
	Article        *fetchArticle `json:"article,omitempty"`
	ReplyCount     int           `json:"reply_count"`
	RepostCount    int           `json:"repost_count"`
	LikeCount      int           `json:"like_count"`
	ViewCount      string        `json:"view_count,omitempty"`
	Media          []fetchMedia  `json:"media,omitempty"`
	Liked          bool          `json:"liked"`
	Reposted       bool          `json:"reposted"`
	Bookmarked     bool          `json:"bookmarked"`
	InReplyToID    string        `json:"in_reply_to_id,omitempty"`
	ConversationID string        `json:"conversation_id,omitempty"`
	Replies        []fetchPost   `json:"replies,omitempty"`
}

func fetchPostFrom(post api.TimelinePost) fetchPost {
	out := fetchPost{
		ID:             post.ID,
		URL:            fmt.Sprintf("https://x.com/%s/status/%s", post.Handle, post.ID),
		Author:         fetchAuthor{Name: post.AuthorName, Handle: post.Handle},
		Text:           post.Text,
		ReplyCount:     post.ReplyCount,
		RepostCount:    post.RepostCount,
		LikeCount:      post.LikeCount,
		ViewCount:      post.ViewCount,
		Liked:          post.Liked,
		Reposted:       post.Reposted,
		Bookmarked:     post.Bookmarked,
		InReplyToID:    post.InReplyToID,
		ConversationID: post.ConversationID,
	}
	if !post.CreatedAt.IsZero() {
		out.CreatedAt = post.CreatedAt.UTC().Format(time.RFC3339)
	}
	if post.Article != nil {
		out.Article = &fetchArticle{Title: post.Article.Title, Text: post.Article.Text}
	}
	for _, media := range post.Media {
		out.Media = append(out.Media, fetchMedia{URL: media.URL, Type: media.Type, AltText: media.AltText})
	}
	return out
}

func renderFetchJSON(focal api.TimelinePost, replies []api.TimelinePost) ([]byte, error) {
	out := fetchPostFrom(focal)
	for _, reply := range replies {
		out.Replies = append(out.Replies, fetchPostFrom(reply))
	}
	return json.MarshalIndent(out, "", "  ")
}

func renderFetchText(focal api.TimelinePost, replies []api.TimelinePost) string {
	var b strings.Builder
	writePost := func(post api.TimelinePost, indent string) {
		stamp := ""
		if !post.CreatedAt.IsZero() {
			stamp = " · " + post.CreatedAt.UTC().Format("2006-01-02 15:04 UTC")
		}
		fmt.Fprintf(&b, "%s%s (@%s)%s\n%s\n", indent, post.AuthorName, post.Handle, stamp, indent+post.Text)
		if post.Article != nil {
			if post.Article.Title != "" {
				fmt.Fprintf(&b, "%s\n%s[Article: %s]\n", indent, indent, post.Article.Title)
			}
			fmt.Fprintf(&b, "%s%s\n", indent, post.Article.Text)
		}
		fmt.Fprintf(&b, "%s♥ %d · ⟳ %d · 💬 %d", indent, post.LikeCount, post.RepostCount, post.ReplyCount)
		if post.ViewCount != "" {
			fmt.Fprintf(&b, " · views %s", post.ViewCount)
		}
		fmt.Fprintf(&b, "\n%shttps://x.com/%s/status/%s\n", indent, post.Handle, post.ID)
	}
	writePost(focal, "")
	for _, reply := range replies {
		fmt.Fprintln(&b)
		writePost(reply, "  ↳ ")
	}
	return b.String()
}

// tweetIDFromArg accepts a bare status id or an x.com/twitter.com status URL
// and returns the id. Host matching is exact after stripping www./mobile. —
// a lookalike domain must fail rather than fetch under our session.
func tweetIDFromArg(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", fmt.Errorf("pass a post URL or status id")
	}
	if isStatusID(arg) {
		return arg, nil
	}
	parsed, err := url.Parse(arg)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", arg, err)
	}
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "mobile.")
	if host != "x.com" && host != "twitter.com" {
		return "", fmt.Errorf("not an x.com or twitter.com URL: %s", arg)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i, part := range parts {
		if (part == "status" || part == "statuses") && i+1 < len(parts) && isStatusID(parts[i+1]) {
			return parts[i+1], nil
		}
	}
	return "", fmt.Errorf("no status id found in URL: %s", arg)
}

func isStatusID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func runFetch(cmd *cobra.Command, args []string) error {
	id, err := tweetIDFromArg(args[0])
	if err != nil {
		return err
	}
	configMgr, err := config.NewConfigManager()
	if err != nil {
		return err
	}
	selector, err := accountSelectorFrom(cmd, fetchAccount)
	if err != nil {
		return err
	}
	cfg, err := loadAccountSelection(configMgr, selector)
	if err != nil {
		return err
	}
	if cfg.AuthToken == "" {
		return fmt.Errorf("run 'xeet auth' first")
	}

	client := api.NewWebClient(cfg)
	count := 1
	if fetchReplies > 0 {
		count = fetchReplies + 1
	}
	page, err := client.FetchTweetDetail(cmd.Context(), id, "", count)
	if err != nil {
		return err
	}
	// Cache freshly discovered operation ids so later commands avoid discovery.
	if client.ApplyRefreshedQueryIDs(cfg) {
		_ = configMgr.SaveQueryIDs(cfg)
	}
	if len(page.Posts) == 0 || page.Posts[0].ID != id {
		return fmt.Errorf("post %s not found or not visible to this account", id)
	}
	focal := page.Posts[0].TimelinePost
	var replies []api.TimelinePost
	if fetchReplies > 0 {
		for _, reply := range page.Posts[1:] {
			if len(replies) == fetchReplies {
				break
			}
			replies = append(replies, reply.TimelinePost)
		}
	}

	if fetchText {
		fmt.Fprint(cmd.OutOrStdout(), renderFetchText(focal, replies))
		return nil
	}
	data, err := renderFetchJSON(focal, replies)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
