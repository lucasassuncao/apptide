package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lucasassuncao/apptide/internal/output"
	"github.com/lucasassuncao/bezel/table"
	"github.com/lucasassuncao/bezel/theme"
	"github.com/spf13/cobra"
)

// Version is set at build time via:
//
//	go build -ldflags "-X github.com/lucasassuncao/apptide/cmd.Version=v1.0.0"
var Version = "dev"

var (
	configPath   string
	outputFormat string
)

// th is the terminal palette the TUI draws with, so plain command output
// reads in the same colours.
var th = theme.Resolve(theme.ThemeTerminal, true)

// rule is a dim line as wide as a table laid out in widths, indent included.
func rule(indent int, widths []int) string {
	w := indent + table.Gap*(len(widths)-1)
	for _, cw := range widths {
		w += cw
	}
	return th.Dim.Render(strings.Repeat("─", w))
}

var rootCmd = &cobra.Command{
	Use:   "apptide",
	Short: "Package installer for Windows",
	Long:  "Install and manage software packages via winget, chocolatey, scoop and github releases.",
	// Both are silenced because Execute prints the error itself; leaving
	// SilenceErrors off made cobra print every failure a second time.
	SilenceUsage:  true,
	SilenceErrors: true,
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

// configSearchPath returns the locations checked, in order, when --config is
// not given. The first existing file wins.
//
// The binary's own directory comes last and only for backward compatibility:
// installed through scoop or winget, apptide lives in a shim directory that is
// replaced on every upgrade, which took the config down with it.
func configSearchPath() []string {
	var paths []string

	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, "packages.yaml"))
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		paths = append(paths, filepath.Join(appData, "apptide", "packages.yaml"))
	}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "conf", "packages.yaml"))
	}
	return paths
}

// resolveConfigPath picks the first config that exists. When none does it
// returns the preferred location, so the failure names where a config is
// expected rather than wherever the binary happens to sit.
func resolveConfigPath() string {
	paths := configSearchPath()
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(paths) > 0 {
		return paths[0]
	}
	return "packages.yaml"
}

// DefaultConfigPath reports where a config would be read from right now.
// Commands that write a config use it so init and install agree.
func DefaultConfigPath() string { return resolveConfigPath() }

func init() {
	rootCmd.Version = Version
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "",
		"Path to configuration file. Defaults to the first of ./packages.yaml, "+
			"%APPDATA%\\apptide\\packages.yaml, <binary-dir>\\conf\\packages.yaml")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "output format: table, json")
	// Propagate the flag value to the output helper before any command runs.
	cobra.OnInitialize(func() {
		output.Set(outputFormat)
		if configPath == "" {
			configPath = resolveConfigPath()
		}
	})
}
