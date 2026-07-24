package cmd

import (
	"fmt"
	"os"

	"github.com/melqtx/xeet/pkg/api"

	"github.com/spf13/cobra"
)

const maxHARBytes = 64 << 20

var inspectHARCmd = &cobra.Command{
	Use:   "inspect-har <file>",
	Short: "compare a browser request with xeet's, secrets redacted",
	Long: `Inspect a HAR exported from browser developer tools and compare the last
CreateTweet POST with the request Xeet currently builds.

The report contains only header names, cookie names, JSON key names, and a
coarse browser family and major version. It never prints header values, cookie
values, post text, query ids, or response bodies. The HAR itself is sensitive,
so keep it local.`,
	Example: "  xeet inspect-har ~/Downloads/x.com.har",
	Args:    cobra.ExactArgs(1),
	RunE:    runInspectHAR,
}

func init() {
	rootCmd.AddCommand(inspectHARCmd)
}

func runInspectHAR(cmd *cobra.Command, args []string) error {
	file, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxHARBytes {
		return fmt.Errorf("HAR is larger than %d MiB", maxHARBytes>>20)
	}

	comparison, err := api.CompareCreateTweetHAR(file)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), comparison.String())
	return nil
}
