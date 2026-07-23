package cmd

import (
	"fmt"
	"os"

	"xeet/internal/tui"
	"xeet/pkg/config"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var (
	appVersion   string
	appCommit    string
	appBuildTime string
)

var rootCmd = &cobra.Command{
	Use:   "xeet",
	Short: "Terminal interface for posting to X.com",
	RunE:  runTUI,
}

func Execute() error { return rootCmd.Execute() }

func SetVersion(version, commit, buildTime string) {
	appVersion = version
	appCommit = commit
	appBuildTime = buildTime
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.xeet.yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
}

func runTUI(cmd *cobra.Command, args []string) error {
	configMgr, err := config.NewConfigManager()
	if err == nil {
		if cfg, loadErr := configMgr.Load(); loadErr == nil && cfg.AuthToken == "" {
			fmt.Println("Welcome to xeet! First, connect your X account:")
			fmt.Println("  xeet auth")
			return nil
		}
	}
	return tui.Run()
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".xeet")
	}
	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err == nil && viper.GetBool("verbose") {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
