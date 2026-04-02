package cmd

import (
	"fmt"
	"os"

	"github.com/lucasassuncao/apptide/internal/output"
	"github.com/spf13/cobra"
)

var (
	configPath   string
	outputFormat string
)

var rootCmd = &cobra.Command{
	Use:          "apptide",
	Short:        "Package installer for Windows",
	Long:         "Install and manage software packages via winget, chocolatey, scoop, github, and third-party sources.",
	SilenceUsage: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "packages.yaml", "path to packages config file")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "output format: table, json")
	// Propagate the flag value to the output helper before any command runs.
	cobra.OnInitialize(func() { output.Set(outputFormat) })
	rootCmd.AddCommand(newVersionCmd())
}
