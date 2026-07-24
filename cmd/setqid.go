package cmd

import (
	"fmt"

	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var setqidCmd = &cobra.Command{
	Use:   "setqid <query-id>",
	Short: "set the CreateTweet query id by hand, if discovery fails",
	Long: `xeet normally discovers the CreateTweet query id on its own. If that fails,
grab it yourself: open x.com, post a tweet, and in DevTools > Network find the
'CreateTweet' request. The id is the segment in its URL:

  https://x.com/i/api/graphql/<THIS-PART>/CreateTweet

Then run:  xeet setqid <THIS-PART>`,
	Args:   cobra.ExactArgs(1),
	Hidden: true, // advanced escape hatch; keep it out of the first-run help
	RunE: func(cmd *cobra.Command, args []string) error {
		configMgr, err := config.NewConfigManager()
		if err != nil {
			return err
		}
		cfg, err := configMgr.Load()
		if err != nil {
			return err
		}
		cfg.CreateTweetQID = args[0]
		if err := configMgr.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("saved CreateTweet query id: %s\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(setqidCmd)
}
