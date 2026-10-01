package cmd

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/bezel/table"
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

// severity separates problems that stop a run from ones that only mean a field
// will be quietly ignored. Warnings are the more valuable half: today a pinned
// version under scoop, or github args without run_installer, fail in silence.
type severity uint8

const (
	sevError severity = iota
	sevWarning
)

type validationIssue struct {
	severity severity
	category string
	app      string
	field    string
	message  string
}

func runValidate(cfgPath string) error {
	cfg, err := config.LoadWithImports(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", th.Danger.Render("error:"), err)
		return err
	}

	if cfg.SchemaVersion < config.SchemaVersion {
		fmt.Fprintf(os.Stderr,
			"%s %q uses the v%d layout; apptide reads it but writes v%d — see the migration notes\n",
			th.Warning.Render("warn:"), cfgPath, cfg.SchemaVersion, config.SchemaVersion)
	}

	var issues []validationIssue
	for _, app := range cfg.Applications {
		issues = append(issues, validateApplication(app)...)
	}

	errCount := 0
	for _, i := range issues {
		if i.severity == sevError {
			errCount++
		}
	}

	if len(issues) == 0 {
		fmt.Printf("%s  config valid — %d application(s) in %d categor(ies)\n",
			th.Success.Render("✓"), len(cfg.Applications), len(cfg.Categories()))
		return nil
	}

	printIssues(issues, len(cfg.Applications), len(cfg.Categories()))
	if errCount > 0 {
		return fmt.Errorf("%d validation error(s)", errCount)
	}
	return nil
}

func validateApplication(app config.Application) []validationIssue {
	label := app.Name
	if label == "" {
		label = "(unnamed)"
	}
	cat := app.CategoryOrDefault()

	var issues []validationIssue
	add := func(sev severity, field, msg string) {
		issues = append(issues, validationIssue{sev, cat, label, field, msg})
	}

	if app.Name == "" {
		add(sevError, "name", "required")
	}
	if !app.EffectiveAction().Valid() {
		add(sevError, "action", fmt.Sprintf("%q is not valid (install, uninstall, skip)", app.Action))
	}

	if len(app.Source) == 0 {
		add(sevError, "source", "required")
		return issues
	}

	seen := map[config.Source]bool{}
	for _, src := range app.Source {
		if !src.Valid() {
			add(sevError, "source", fmt.Sprintf("%q is not valid (winget, chocolatey, scoop, github)", src))
			continue
		}
		if seen[src] {
			add(sevError, "source", fmt.Sprintf("%q listed more than once", src))
			continue
		}
		seen[src] = true

		if id, ok := app.Package.ID(src); !ok || id == "" {
			add(sevError, fmt.Sprintf("package.%s.id", src), fmt.Sprintf("required — %q is listed in source", src))
		}
	}

	// A block for a source that is not listed will never be used.
	for _, src := range app.Package.Configured() {
		if !seen[src] {
			add(sevWarning, "package."+string(src),
				fmt.Sprintf("configured but %q is not in source — this block is never used", src))
		}
	}

	issues = append(issues, validateSourceQuirks(app, cat, label)...)
	return issues
}

// validateSourceQuirks reports fields that a given source silently ignores.
func validateSourceQuirks(app config.Application, cat, label string) []validationIssue {
	var issues []validationIssue
	add := func(sev severity, field, msg string) {
		issues = append(issues, validationIssue{sev, cat, label, field, msg})
	}

	pinned := app.Version != "" && !strings.EqualFold(app.Version, "latest")
	if pinned && app.Source.Contains(config.SourceScoop) {
		add(sevWarning, "version",
			"scoop cannot install a specific version — it would install latest instead")
	}

	onlyGitHub := len(app.Source) == 1 && app.Source[0] == config.SourceGitHub
	if app.SkipUpgrade && onlyGitHub {
		add(sevWarning, "skip_upgrade", "has no effect: the github source never upgrades")
	}

	if app.Source.Contains(config.SourceGitHub) && app.EffectiveAction() == config.ActionUninstall {
		add(sevError, "action", "uninstall is not supported for the github source")
	}

	if gh := app.Package.GitHub; gh != nil {
		if len(gh.Args) > 0 && !gh.RunInstaller {
			add(sevWarning, "package.github.args",
				"only used with run_installer: true — ignored otherwise")
		}
		if gh.ID != "" && !strings.Contains(gh.ID, "/") {
			add(sevError, "package.github.id", fmt.Sprintf("%q is not in owner/repo form", gh.ID))
		}
	}

	return issues
}

func printIssues(issues []validationIssue, total, numCategories int) {
	cols := []table.Column{{}, {Title: "Application"}, {Title: "Category"}, {Title: "Field"}, {Title: "Problem"}}
	rows := make([][]string, len(issues))
	var errs, warns int
	for i, e := range issues {
		mark, style := th.Danger.Render("✗"), th.Danger
		if e.severity == sevWarning {
			mark, style = th.Warning.Render("!"), th.Warning
			warns++
		} else {
			errs++
		}
		rows[i] = []string{mark, e.app, th.Dim.Render(e.category), th.Warning.Render(e.field), style.Render(e.message)}
	}
	widths := table.Fit(cols, rows, math.MaxInt)
	sep := rule(2, widths)

	fmt.Println("\n  " + th.Bold.Render(table.Titles(cols, widths)))
	fmt.Println(sep)
	for _, r := range rows {
		fmt.Println("  " + table.Row(widths, r))
	}

	fmt.Println("\n" + sep)
	fmt.Printf("%s, %s in %d application(s) across %d categor(ies)\n",
		th.Danger.Render(fmt.Sprintf("%d error(s)", errs)), th.Warning.Render(fmt.Sprintf("%d warning(s)", warns)),
		total, numCategories)
}
