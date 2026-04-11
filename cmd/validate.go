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
	installer.SourceScoop: true, installer.SourceGitHub: true,
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

	categories := cfg.Categories()
	total := countPackages(cfg, categories)
	issues := collectIssues(cfg, categories)

	if len(issues) == 0 {
		fmt.Printf("%s✓%s  config valid — %d package(s) in %d categor(ies)\n",
			vlGreen, vlReset, total, len(categories))
		return nil
	}

	printIssues(issues, total, len(categories))
	return fmt.Errorf("%d validation error(s)", len(issues))
}

func countPackages(cfg config.Config, categories []string) int {
	total := 0
	for _, cat := range categories {
		total += len(cfg[cat])
	}
	return total
}

func collectIssues(cfg config.Config, categories []string) []validationIssue {
	var issues []validationIssue
	for _, cat := range categories {
		for _, pkg := range cfg[cat] {
			issues = append(issues, validatePackage(cat, pkg)...)
		}
	}
	return issues
}

func validatePackage(cat string, pkg config.Package) []validationIssue {
	var issues []validationIssue

	label := pkg.Name
	if label == "" {
		label = "(unnamed)"
	}

	if pkg.Name == "" {
		issues = append(issues, validationIssue{cat, label, "name", "required"})
	}

	issues = append(issues, validateSource(cat, label, pkg)...)
	issues = append(issues, validateAction(cat, label, pkg)...)

	return issues
}

func validateSource(cat, label string, pkg config.Package) []validationIssue {
	var issues []validationIssue
	src := strings.ToLower(pkg.Source)

	switch {
	case pkg.Source == "":
		issues = append(issues, validationIssue{cat, label, "source", "required"})
	case !knownSources[src]:
		issues = append(issues, validationIssue{
			cat, label, "source",
			fmt.Sprintf("%q is not valid (winget, chocolatey, scoop, github)", pkg.Source),
		})
	default:
		issues = append(issues, validateSourceBlock(cat, label, src, pkg)...)
	}

	return issues
}

func validateSourceBlock(cat, label, src string, pkg config.Package) []validationIssue {
	switch src {
	case installer.SourceWinget:
		if pkg.Winget == nil || pkg.Winget.ID == "" {
			return []validationIssue{{cat, label, "winget.id", "required for winget"}}
		}
	case installer.SourceChocolatey, "choco":
		if pkg.Chocolatey == nil || pkg.Chocolatey.ID == "" {
			return []validationIssue{{cat, label, "chocolatey.id", "required for chocolatey"}}
		}
	case installer.SourceScoop:
		if pkg.Scoop == nil || pkg.Scoop.ID == "" {
			return []validationIssue{{cat, label, "scoop.id", "required for scoop"}}
		}
	case installer.SourceGitHub:
		if pkg.GitHub == nil || pkg.GitHub.Repo == "" {
			return []validationIssue{{cat, label, "github.repo", "required for github"}}
		}
	}
	return nil
}

func validateAction(cat, label string, pkg config.Package) []validationIssue {
	if !knownActions[strings.ToLower(pkg.Action)] {
		return []validationIssue{{
			cat, label, "action",
			fmt.Sprintf("%q is not valid (install, uninstall, skip)", pkg.Action),
		}}
	}
	return nil
}

func printIssues(issues []validationIssue, total, numCategories int) {
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
		vlRed, len(issues), vlReset, total, numCategories)
}
