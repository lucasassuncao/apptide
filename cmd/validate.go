package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the config file for errors and missing required fields",
	Example: `  apptide validate
  apptide validate --config other.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runValidate(configPath)
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

const (
	vlReset  = "\033[0m"
	vlBold   = "\033[1m"
	vlRed    = "\033[31m"
	vlGreen  = "\033[32m"
	vlYellow = "\033[33m"
	vlGray   = "\033[90m"
)

type validationIssue struct {
	category string
	pkg      string
	field    string
	message  string
}

var knownSources = map[string]bool{
	installer.SourceWinget: true, installer.SourceChocolatey: true, "choco": true,
	installer.SourceScoop: true, installer.SourceGitHub: true, installer.SourceThirdParty: true,
}

var knownActions = map[string]bool{
	"install": true, "uninstall": true, "skip": true, "": true,
}

func runValidate(cfgPath string) error {
	cfg, err := config.LoadWithImports(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%serror:%s %v\n", vlRed, vlReset, err)
		return err
	}

	var issues []validationIssue

	for _, cat := range cfg.Categories() {
		for _, pkg := range cfg[cat] {
			label := pkg.Name
			if label == "" {
				label = "(unnamed)"
			}

			if pkg.Name == "" {
				issues = append(issues, validationIssue{cat, label, "name", "required"})
			}

			src := strings.ToLower(pkg.Source)
			if pkg.Source == "" {
				issues = append(issues, validationIssue{cat, label, "source", "required"})
			} else if !knownSources[src] {
				issues = append(issues, validationIssue{
					cat, label, "source",
					fmt.Sprintf("%q is not valid (winget, chocolatey, scoop, github, third_party)", pkg.Source),
				})
			}

			if !knownActions[strings.ToLower(pkg.Action)] {
				issues = append(issues, validationIssue{
					cat, label, "action",
					fmt.Sprintf("%q is not valid (install, uninstall, skip)", pkg.Action),
				})
			}

			switch src {
			case installer.SourceWinget, installer.SourceChocolatey, "choco", installer.SourceScoop:
				if pkg.ID == "" {
					issues = append(issues, validationIssue{cat, label, "id", "required for " + pkg.Source})
				}
			case installer.SourceGitHub:
				if pkg.Repo == "" {
					issues = append(issues, validationIssue{cat, label, "repo", "required for github"})
				}
			case installer.SourceThirdParty:
				if pkg.URL == "" {
					issues = append(issues, validationIssue{cat, label, "url", "required for third_party"})
				}
			}
		}
	}

	categories := cfg.Categories()
	total := 0
	for _, cat := range categories {
		total += len(cfg[cat])
	}

	if len(issues) == 0 {
		fmt.Printf("%s✓%s  config valid — %d package(s) in %d categor(ies)\n",
			vlGreen, vlReset, total, len(categories))
		return nil
	}

	fmt.Printf("\n  %s%-28s  %-20s  %-14s  %s%s\n",
		vlBold, "Package", "Category", "Field", "Problem", vlReset)
	sep := vlGray + strings.Repeat("─", 80) + vlReset
	fmt.Println(sep)

	for _, e := range issues {
		fmt.Printf("  %s✗%s  %-28s  %s%-20s%s  %s%-14s%s  %s%s%s\n",
			vlRed, vlReset,
			e.pkg,
			vlGray, e.category, vlReset,
			vlYellow, e.field, vlReset,
			vlRed, e.message, vlReset,
		)
	}

	fmt.Println("\n" + sep)
	fmt.Printf("%s%d error(s)%s in %d package(s) across %d categor(ies)\n",
		vlRed, len(issues), vlReset, total, len(categories))

	return fmt.Errorf("%d validation error(s)", len(issues))
}
