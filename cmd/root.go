package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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

// resolveToken returns the explicit flag value when set, otherwise falls back
// to $GITHUB_TOKEN. The env var is read at call time (not at flag parse time)
// to avoid exposing the token value in --help output.
func resolveToken(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("GITHUB_TOKEN")
}

// defaultConfigPath returns <binary-dir>/conf/packages.yaml.
func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join("conf", "packages.yaml")
	}
	return filepath.Join(filepath.Dir(exe), "conf", "packages.yaml")
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", defaultConfigPath(), "path to packages config file")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "output format: table, json")
	// Propagate the flag value to the output helper before any command runs.
	cobra.OnInitialize(func() { output.Set(outputFormat) })
	rootCmd.AddCommand(newVersionCmd())
}
