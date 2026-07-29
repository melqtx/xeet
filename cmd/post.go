package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/melqtx/xeet/internal/media"
	"github.com/melqtx/xeet/pkg/api"
	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var (
	replyTo     string
	quoteTo     string
	imagePaths  []string
	postAccount string
)

var postCmd = &cobra.Command{
	Use:   "post [text]",
	Short: "post from the command line, or from a pipe",
	Long: `Post straight from the command line, with no interface in the way. The text
can be an argument or piped in on stdin; attachments are up to four images or
a single video.

--account decides which saved account posts, resolved inside this one command
so nothing else on the machine can move it in between. It only decides; it does
not judge. A script that used to compare its expected handle against the active
account was getting a wrong-account check for free, because a mistaken expected
value disagreed with whatever was active. Passing that same value to --account
turns it into the destination instead, so the check has to move to the caller.`,
	Example: `  xeet post "hello world"
  echo "piped tweet" | xeet post
  xeet post "photo" --image ./photo.png
  xeet post --image one.png --image two.jpg   # no text, images only
  xeet post "clip" --image ./clip.mp4
  xeet post "a reply" --reply 1234567890
  xeet post "a quote" --quote 1234567890
  xeet post "hi" --account @alice        # post as a saved account, not the active one`,
	RunE: runPost,
}

func init() {
	postCmd.Flags().StringVar(&replyTo, "reply", "", "tweet id to reply to")
	postCmd.Flags().StringVar(&quoteTo, "quote", "", "tweet id to quote")
	postCmd.Flags().StringArrayVarP(&imagePaths, "image", "i", nil, "image or video path (up to 4 images, or 1 video)")
	postCmd.Flags().StringVar(&postAccount, "account", "", "saved account to post as (handle or user id); defaults to the active one")
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
	videos := 0
	for _, path := range imagePaths {
		attachment, err := media.FromPath(path)
		if err != nil {
			return fmt.Errorf("attach %q: %w", path, err)
		}
		upload := api.Upload{Filename: attachment.Name, ContentType: attachment.MIME, Data: attachment.Data}
		if attachment.IsVideo() {
			videos++
			upload.Path = attachment.Path
		}
		uploads = append(uploads, upload)
	}
	if videos > 0 && len(uploads) > 1 {
		return fmt.Errorf("a video must be the only attachment")
	}
	if strings.TrimSpace(text) == "" && len(uploads) == 0 {
		return fmt.Errorf("nothing to post: pass text, pipe stdin, or add --image")
	}

	configMgr, err := config.NewConfigManager()
	if err != nil {
		return err
	}
	selector, err := accountSelectorFrom(cmd, postAccount)
	if err != nil {
		return err
	}
	cfg, err := loadAccountSelection(configMgr, selector)
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	if cfg.AuthToken == "" {
		return fmt.Errorf("run 'xeet auth' first")
	}

	client := api.NewWebClient(cfg)
	id, err := client.PostTweet(ctx, text, replyTo, quoteTo, uploads, func(event api.PostEvent) {
		if v, _ := cmd.Flags().GetBool("verbose"); v {
			switch event.Stage {
			case api.PostStageUploading:
				if event.TotalBytes > 0 {
					fmt.Fprintf(os.Stderr, "uploading %s: %d%%\n", event.Name, event.TransferredBytes*100/event.TotalBytes)
				} else {
					fmt.Fprintf(os.Stderr, "uploading %d/%d: %s\n", event.Current, event.Total, event.Name)
				}
			case api.PostStageProcessing:
				fmt.Fprintln(os.Stderr, "waiting for X to process the media…")
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
		_ = configMgr.SaveQueryIDs(cfg)
	}
	if id != "" {
		fmt.Printf("posted: https://x.com/i/status/%s\n", id)
	} else {
		fmt.Println("posted")
	}
	return nil
}
