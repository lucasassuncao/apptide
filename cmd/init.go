package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var (
	initInteractive bool
	initTemplate    string
	initForce       bool
	initOutputFile  string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a new packages.yaml configuration file",
	Long: `Create a packages.yaml from scratch using an interactive wizard or a predefined template.

Templates:
  minimal   Bare structure with field reference (default)
  example   One application per source type as a working starting point`,
	Example: `  apptide init                         # minimal template → packages.yaml
  apptide init -i                      # interactive wizard
  apptide init -t example              # example template
  apptide init -o work-packages.yaml   # custom output path
  apptide init -i --force              # overwrite existing file`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Guard: don't overwrite without --force.
		if _, err := os.Stat(initOutputFile); err == nil && !initForce {
			return fmt.Errorf("%q already exists — use --force to overwrite", initOutputFile)
		}

		if initInteractive {
			return runInitWizard()
		}
		return runInitTemplate()
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().BoolVarP(&initInteractive, "interactive", "i", false, "run the interactive configuration wizard")
	initCmd.Flags().StringVarP(&initTemplate, "template", "t", "minimal", "template: minimal | example")
	initCmd.Flags().BoolVarP(&initForce, "force", "f", false, "overwrite an existing file")
	initCmd.Flags().StringVarP(&initOutputFile, "output", "o", "packages.yaml", "output file path")
}

// ── Wizard ────────────────────────────────────────────────────────────────────

func runInitWizard() error {
	clearScreen()

	// ── Header ───────────────────────────────────────────────────────────────
	pterm.DefaultHeader.WithFullWidth().
		WithBackgroundStyle(pterm.NewStyle(pterm.BgDarkGray)).
		WithTextStyle(pterm.NewStyle(pterm.FgLightWhite)).
		Println("  apptide · Configuration Wizard  ")
	pterm.Println()

	pterm.Info.Printfln("Creating: %s", pterm.Cyan(initOutputFile))
	pterm.Println()

	// ── Sources ───────────────────────────────────────────────────────────────
	pterm.DefaultSection.Println("Package Sources")

	allSources := make([]string, 0, len(config.AllSources()))
	for _, s := range config.AllSources() {
		allSources = append(allSources, string(s))
	}

	selectedSources, err := pterm.DefaultInteractiveMultiselect.
		WithOptions(allSources).
		WithDefaultText("Which package managers do you use?").
		WithCheckmark(&pterm.Checkmark{Checked: "+", Unchecked: " "}).
		Show()
	if err != nil {
		return err
	}
	if len(selectedSources) == 0 {
		pterm.Warning.Println("No sources selected — generating empty config.")
	}
	pterm.Println()

	// ── Applications ──────────────────────────────────────────────────────────
	pterm.DefaultSection.Println("Applications")

	var apps []config.Application
	var categories []string // ordered list of category names seen so far

	addNow, err := pterm.DefaultInteractiveConfirm.
		WithDefaultText("Add applications now?").
		WithDefaultValue(true).
		Show()
	if err != nil {
		return err
	}

	if addNow {
		if err := collectApplications(&apps, &categories, selectedSources, allSources); err != nil {
			return err
		}
	}

	// ── Write file ────────────────────────────────────────────────────────────
	clearScreen()

	if err := writeWizardResult(initOutputFile, apps); err != nil {
		return err
	}

	pterm.Println()
	pterm.Success.Printfln("%s created with %s",
		pterm.Cyan(initOutputFile),
		pterm.Bold.Sprintf("%d application(s)", len(apps)))
	pterm.Println()
	pterm.DefaultBox.
		WithTitle("Next steps").
		Println(strings.Join([]string{
			"Review and edit: " + pterm.Gray("apptide edit"),
			"List packages:   " + pterm.Gray("apptide list"),
			"Install all:     " + pterm.Gray("apptide install"),
			"Verify setup:    " + pterm.Gray("apptide verify"),
		}, "\n"))
	pterm.Println()

	return nil
}

// collectApplications runs the interactive loop that builds the application list.
func collectApplications(apps *[]config.Application, categories *[]string, selectedSources, allSources []string) error {
	for {
		pterm.Println()
		pterm.DefaultSection.Printfln("Application %d", len(*apps)+1)

		catName, err := selectOrCreateCategory(categories)
		if err != nil {
			return err
		}

		app, err := promptApplication(selectedSources, allSources)
		if err != nil {
			return err
		}
		if app == nil {
			continue
		}
		app.Category = catName

		pterm.Println()
		summaryLines := []string{
			fmt.Sprintf("%s  [%s]  →  %s",
				pterm.Bold.Sprint(app.Name), pterm.Cyan(app.Source.String()), pterm.Yellow(catName)),
			packageIDLine(*app),
			fmt.Sprintf("action: %s", app.EffectiveAction()),
		}
		if app.Description != "" {
			summaryLines = append(summaryLines, pterm.Gray(app.Description))
		}
		pterm.DefaultBox.
			WithTitle(pterm.Green("✓ Added")).
			Println(strings.Join(summaryLines, "\n"))

		*apps = append(*apps, *app)

		more, err := pterm.DefaultInteractiveConfirm.
			WithDefaultText("Add another application?").
			WithDefaultValue(true).
			Show()
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	return nil
}

// promptApplication collects name, source, source-specific fields, description
// and action. Returns nil if the name is empty (caller should skip and continue).
func promptApplication(selectedSources, allSources []string) (*config.Application, error) {
	name, err := pterm.DefaultInteractiveTextInput.
		WithDefaultText("Name").
		Show()
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		pterm.Warning.Println("Name cannot be empty — skipping.")
		return nil, nil
	}

	sourceOptions := selectedSources
	if len(sourceOptions) == 0 {
		sourceOptions = allSources
	}
	src, err := pterm.DefaultInteractiveSelect.
		WithOptions(sourceOptions).
		WithDefaultText("Source").
		Show()
	if err != nil {
		return nil, err
	}

	app := config.Application{
		Name:   name,
		Source: config.Sources{config.Source(src).Normalize()},
		Action: config.ActionInstall,
	}

	if err := promptSourceFields(&app); err != nil {
		return nil, err
	}

	desc, err := pterm.DefaultInteractiveTextInput.
		WithDefaultText("Description (optional)").
		Show()
	if err != nil {
		return nil, err
	}
	app.Description = strings.TrimSpace(desc)

	action, err := pterm.DefaultInteractiveSelect.
		WithOptions([]string{string(config.ActionInstall), string(config.ActionSkip)}).
		WithDefaultText("Action").
		Show()
	if err != nil {
		return nil, err
	}
	app.Action = config.Action(action)

	return &app, nil
}

// promptSourceFields fills the source-specific block of app interactively.
func promptSourceFields(app *config.Application) error {
	src, _ := app.Source.First()

	switch src {
	case config.SourceWinget:
		id, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("ID (winget package identifier)").
			Show()
		if err != nil {
			return err
		}
		app.Package.Winget = &config.WingetSpec{ID: strings.TrimSpace(id)}

	case config.SourceChocolatey:
		id, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("ID (chocolatey package identifier)").
			Show()
		if err != nil {
			return err
		}
		app.Package.Chocolatey = &config.ChocolateySpec{ID: strings.TrimSpace(id)}

	case config.SourceScoop:
		id, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("ID (scoop package identifier)").
			Show()
		if err != nil {
			return err
		}
		app.Package.Scoop = &config.ScoopSpec{ID: strings.TrimSpace(id)}

	case config.SourceGitHub:
		repo, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("ID (owner/repo)").
			Show()
		if err != nil {
			return err
		}

		ver, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Version").
			WithDefaultValue("latest").
			Show()
		if err != nil {
			return err
		}
		app.Package.GitHub = &config.GitHubSpec{ID: strings.TrimSpace(repo)}
		app.Version = strings.TrimSpace(ver)
	}
	return nil
}

// selectOrCreateCategory presents a select with existing categories + a "New" option.
func selectOrCreateCategory(categories *[]string) (string, error) {
	const newOpt = "→  New category"

	options := append([]string{newOpt}, *categories...)
	selected, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText("Category").
		Show()
	if err != nil {
		return "", err
	}

	if selected != newOpt {
		return selected, nil
	}

	name, err := pterm.DefaultInteractiveTextInput.
		WithDefaultText("Category name").
		Show()
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "General"
	}
	// Only append if not already present.
	for _, c := range *categories {
		if strings.EqualFold(c, name) {
			return c, nil
		}
	}
	*categories = append(*categories, name)
	return name, nil
}

// packageIDLine returns a short identifier line for the summary box.
func packageIDLine(app config.Application) string {
	src, ok := app.Source.First()
	if !ok {
		return ""
	}
	id, ok := app.Package.ID(src)
	if !ok || id == "" {
		return ""
	}
	line := fmt.Sprintf("id: %s", id)
	if src == config.SourceGitHub && app.Version != "" {
		line += fmt.Sprintf("  @%s", app.Version)
	}
	return line
}

// writeWizardResult writes the collected applications to path as a YAML file.
func writeWizardResult(path string, apps []config.Application) error {
	f, err := os.Create(path) //#nosec G304 -- the output path is the user's own choice
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()

	fmt.Fprintf(f, "# Generated by apptide init on %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(f, "# Run: apptide install\n\n")
	fmt.Fprintf(f, "schema_version: %d\n\napplications:\n", config.SchemaVersion)

	for _, app := range apps {
		writeApplicationEntry(f, app)
	}
	return nil
}

// ── Templates ─────────────────────────────────────────────────────────────────

// initTemplates maps a template name to the function that renders it. Both
// `init --template` and the document picker in `apptide edit` read this map,
// so the two can never offer different templates.
func initTemplates() map[string]func() string {
	return map[string]func() string{
		"minimal": templateMinimal,
		"example": templateExample,
	}
}

// initTemplateNames lists the templates in a stable order.
func initTemplateNames() []string { return sortedNames(initTemplates()) }

func runInitTemplate() error {
	name := initTemplate
	if name == "" {
		name = "minimal"
	}
	render, ok := initTemplates()[name]
	if !ok {
		return fmt.Errorf("unknown template %q — valid: %s", initTemplate, strings.Join(initTemplateNames(), ", "))
	}
	content := render()

	if err := os.WriteFile(initOutputFile, []byte(content), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", initOutputFile, err)
	}

	pterm.Success.Printfln("%s created (template: %s)", pterm.Cyan(initOutputFile), name)
	pterm.Info.Println("Edit the file with: apptide edit")
	return nil
}

func templateMinimal() string {
	return fmt.Sprintf(`# apptide config — generated %s
# Edit interactively with: apptide edit
#
# Fields reference:
#   name          : display name (required)
#   source        : one source, or a preference list — [winget, scoop, github]
#                   later entries are fallbacks, used only when an earlier
#                   source does not offer the package at all
#   category      : free-form group, used by --category
#   action        : install | uninstall | skip  (default: install)
#   version       : exact version or "latest"
#   skip_upgrade  : true → install if missing, never upgrade
#   hooks         : pre_install / post_install shell commands
#
#   package.winget:      id: "Publisher.App"
#   package.chocolatey:  id: "package-name"
#   package.scoop:       id: "package-name"
#   package.github:      id: "owner/repo"   binary_name: "gh"

schema_version: %d

applications:
  - name: "Example App"
    category: MyApps
    source: winget
    description: "Replace this with your first application"
    package:
      winget:
        id: "Publisher.AppId"
`, time.Now().Format("2006-01-02"), config.SchemaVersion)
}

func templateExample() string {
	return fmt.Sprintf(`# apptide config — generated %s
# One application per source type. Edit with: apptide edit

schema_version: %d

applications:
  - name: "Git"
    category: Development
    source: winget
    description: "Distributed version control system"
    package:
      winget:
        id: "Git.Git"

  - name: "Vim"
    category: Development
    source: scoop
    description: "Highly configurable text editor"
    package:
      scoop:
        id: "vim"

  - name: "Clink"
    category: Development
    source: chocolatey
    description: "Powerful readline editing for cmd.exe"
    package:
      chocolatey:
        id: "clink"

  # Preference list: winget first, scoop and github as fallbacks.
  # Whichever succeeds is recorded and used for upgrades and removal.
  - name: "Lazygit"
    category: Development
    source: [winget, scoop, github]
    description: "Terminal UI for git"
    package:
      winget:
        id: "JesseDuffield.lazygit"
      scoop:
        id: "lazygit"
      github:
        id: "jesseduffield/lazygit"
        asset_pattern: "*Windows_x86_64.zip"

  - name: "7-Zip"
    category: Utilities
    source: winget
    description: "High-compression file archiver"
    package:
      winget:
        id: "7zip.7zip"

  - name: "jq"
    category: Utilities
    source: winget
    description: "Command-line JSON processor"
    package:
      winget:
        id: "jqlang.jq"
`, time.Now().Format("2006-01-02"), config.SchemaVersion)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// writeApplicationEntry writes a single YAML application block to w.
func writeApplicationEntry(w *os.File, app config.Application) {
	fmt.Fprintf(w, "  - name: %q\n", app.Name)
	if app.Category != "" {
		fmt.Fprintf(w, "    category: %s\n", app.Category)
	}
	fmt.Fprintf(w, "    source: %s\n", app.Source.String())

	if app.Description != "" {
		fmt.Fprintf(w, "    description: %q\n", app.Description)
	}
	if act := app.EffectiveAction(); act != config.ActionInstall {
		fmt.Fprintf(w, "    action: %s\n", act)
	}

	src, ok := app.Source.First()
	if !ok {
		fmt.Fprintln(w)
		return
	}
	if src == config.SourceGitHub && app.Version != "" {
		fmt.Fprintf(w, "    version: %q\n", app.Version)
	}
	if id, ok := app.Package.ID(src); ok && id != "" {
		fmt.Fprintf(w, "    package:\n")
		fmt.Fprintf(w, "      %s:\n", src)
		fmt.Fprintf(w, "        id: %q\n", id)
	}

	fmt.Fprintln(w)
}

func clearScreen() {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "cls")
		cmd.Stdout = os.Stdout
		cmd.Run() //nolint:errcheck
	} else {
		cmd := exec.Command("clear")
		cmd.Stdout = os.Stdout
		cmd.Run() //nolint:errcheck
	}
}
