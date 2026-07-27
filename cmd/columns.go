package cmd

import (
	"fmt"

	"github.com/melqtx/xeet/pkg/config"

	"github.com/spf13/cobra"
)

var columnsCmd = &cobra.Command{
	Use:   "columns",
	Short: "manage the saved multi-column layout",
}

var columnsSaveCmd = &cobra.Command{
	Use:     `save "spec[,spec...]"`,
	Short:   "save the default column feeds",
	Example: `  xeet columns save "foryou,list:123"`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := config.NewConfigManager()
		if err != nil {
			return err
		}
		accounts, err := mgr.Accounts()
		if err != nil {
			return err
		}
		_, canonical, err := parseColumnSpecList(args[0], accounts)
		if err != nil {
			return fmt.Errorf("invalid columns layout %q (%v)", args[0], err)
		}
		if err := mgr.SaveColumns(canonical); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "saved %d columns\n", len(canonical))
		return nil
	},
}

func init() {
	columnsCmd.AddCommand(columnsSaveCmd)
	rootCmd.AddCommand(columnsCmd)
}
