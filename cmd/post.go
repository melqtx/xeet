package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"xeet/internal/media"
	"xeet/pkg/api"
	"xeet/pkg/config"

	"github.com/spf13/cobra"
)

var (
	replyTo    string
	imagePaths []string
)

var postCmd = &cobra.Command{
	Use:   "post [text]",
	Short: "Post a tweet non-interactively (reads stdin if no text given)",
	Long: `Post a tweet straight from the command line.

  xeet post "hello world"
  echo "piped tweet" | xeet post
  xeet post "photo" --image ./photo.png
  xeet post --image one.png --image two.jpg
  xeet post "a reply" --reply 1234567890`,
	RunE: runPost,
}

func init() {
	postCmd.Flags().StringVar(&replyTo, "reply", "", "tweet id to reply to")
	postCmd.Flags().StringArrayVarP(&imagePaths, "image", "i", nil, "image path (repeat up to 4 times)")
	rootCmd.AddCommand(postCmd)
}

func runPost(cmd *cobra.Command, args []string) error {
	text := strings.Join(args, " ")
	if strings.TrimSpace(text) == "" {
		// No positional text: read from stdin (supports piping).
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			piped, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			text = strings.TrimRight(string(piped), "\n")
		}
	}
	if len(imagePaths) > media.MaxAttachments {
		return fmt.Errorf("a post can have at most %d images", media.MaxAttachments)
	}
	uploads := make([]api.Upload, 0, len(imagePaths))
	for _, path := range imagePaths {
		attachment, err := media.FromPath(path)
		if err != nil {
			return fmt.Errorf("attach %q: %w", path, err)
		}
		uploads = append(uploads, api.Upload{
			Filename: attachment.Name, ContentType: attachment.MIME, Data: attachment.Data,
		})
	}
	if strings.TrimSpace(text) == "" && len(uploads) == 0 {
		return fmt.Errorf("nothing to post: pass text, pipe stdin, or add --image")
	}

	configMgr, err := config.NewConfigManager()
	if err != nil {
		return err
	}
	cfg, err := configMgr.Load()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	if cfg.AuthToken == "" {
		return fmt.Errorf("run 'xeet auth' first")
	}

	client := api.NewWebClient(cfg)
	id, err := client.PostTweet(ctx, text, replyTo, uploads, func(event api.PostEvent) {
		if v, _ := cmd.Flags().GetBool("verbose"); v {
			switch event.Stage {
			case api.PostStageUploading:
				fmt.Fprintf(os.Stderr, "uploading %d/%d: %s\n", event.Current, event.Total, event.Name)
			case api.PostStageDiscovering:
				fmt.Fprintln(os.Stderr, "refreshing X endpoint…")
			case api.PostStagePublishing:
				fmt.Fprintln(os.Stderr, "publishing…")
			case api.PostStageReconciling:
				fmt.Fprintln(os.Stderr, "checking whether the post landed…")
			}
		}
	})
	if err != nil {
		if v, _ := cmd.Flags().GetBool("verbose"); v {
			if diagnostic := client.LastDiagnostic(); diagnostic != "" {
				fmt.Fprintf(os.Stderr, "diagnostic: %s\n", diagnostic)
			}
		}
		return err
	}
	// Cache freshly discovered operation ids so later commands avoid discovery.
	if client.ApplyRefreshedQueryIDs(cfg) {
		_ = configMgr.Save(cfg)
	}
	if id != "" {
		fmt.Printf("posted: https://x.com/i/status/%s\n", id)
	} else {
		fmt.Println("posted")
	}
	return nil
}
