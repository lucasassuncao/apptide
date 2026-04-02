package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
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
  example   One package per source type as a working starting point`,
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

// wizardPkg holds everything the wizard collects for one package entry.
type wizardPkg struct {
	category string
	pkg      config.Package
}

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

	allSources := []string{installer.SourceWinget, installer.SourceScoop, installer.SourceChocolatey, installer.SourceGitHub, installer.SourceThirdParty}
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

	// ── Packages ──────────────────────────────────────────────────────────────
	pterm.DefaultSection.Println("Packages")

	var entries []wizardPkg
	var categories []string // ordered list of category names seen so far

	addNow, err := pterm.DefaultInteractiveConfirm.
		WithDefaultText("Add packages now?").
		WithDefaultValue(true).
		Show()
	if err != nil {
		return err
	}

	if addNow {
		if err := collectPackages(&entries, &categories, selectedSources, allSources); err != nil {
			return err
		}
	}

	// ── Write file ────────────────────────────────────────────────────────────
	clearScreen()

	if err := writeWizardResult(initOutputFile, entries, categories); err != nil {
		return err
	}

	pkgCount := len(entries)
	pterm.Println()
	pterm.Success.Printfln("%s created with %s",
		pterm.Cyan(initOutputFile),
		pterm.Bold.Sprintf("%d package(s)", pkgCount))
	pterm.Println()
	pterm.DefaultBox.
		WithTitle("Next steps").
		Println(strings.Join([]string{
			"Review and edit: " + pterm.Cyan(initOutputFile),
			"List packages:   " + pterm.Gray("apptide list"),
			"Install all:     " + pterm.Gray("apptide install"),
			"Verify setup:    " + pterm.Gray("apptide verify"),
		}, "\n"))
	pterm.Println()

	return nil
}

// collectPackages runs the interactive loop that builds the entries slice.
func collectPackages(entries *[]wizardPkg, categories *[]string, selectedSources, allSources []string) error {
	for {
		pterm.Println()
		pterm.DefaultSection.Printfln("Package %d", len(*entries)+1)

		catName, err := selectOrCreateCategory(categories)
		if err != nil {
			return err
		}

		pkg, err := promptPackage(selectedSources, allSources)
		if err != nil {
			return err
		}
		if pkg == nil {
			continue
		}

		pterm.Println()
		summaryLines := []string{
			fmt.Sprintf("%s  [%s]  →  %s", pterm.Bold.Sprint(pkg.Name), pterm.Cyan(pkg.Source), pterm.Yellow(catName)),
			packageIDLine(*pkg),
			fmt.Sprintf("action: %s", pkg.Action),
		}
		if pkg.Description != "" {
			summaryLines = append(summaryLines, pterm.Gray(pkg.Description))
		}
		pterm.DefaultBox.
			WithTitle(pterm.Green("✓ Added")).
			Println(strings.Join(summaryLines, "\n"))

		*entries = append(*entries, wizardPkg{category: catName, pkg: *pkg})

		more, err := pterm.DefaultInteractiveConfirm.
			WithDefaultText("Add another package?").
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

// promptPackage collects name, source, source-specific fields, description, and action.
// Returns nil if the name is empty (caller should skip and continue).
func promptPackage(selectedSources, allSources []string) (*config.Package, error) {
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

	pkg := config.Package{Name: name, Source: src, Action: "install"}

	if err := promptSourceFields(&pkg); err != nil {
		return nil, err
	}

	desc, err := pterm.DefaultInteractiveTextInput.
		WithDefaultText("Description (optional)").
		Show()
	if err != nil {
		return nil, err
	}
	pkg.Description = strings.TrimSpace(desc)

	action, err := pterm.DefaultInteractiveSelect.
		WithOptions([]string{"install", "skip"}).
		WithDefaultText("Action").
		Show()
	if err != nil {
		return nil, err
	}
	pkg.Action = action

	return &pkg, nil
}

// promptSourceFields fills the source-specific fields of pkg interactively.
func promptSourceFields(pkg *config.Package) error {
	switch pkg.Source {
	case installer.SourceWinget, installer.SourceChocolatey, installer.SourceScoop:
		id, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText(fmt.Sprintf("ID (%s package identifier)", pkg.Source)).
			Show()
		if err != nil {
			return err
		}
		pkg.ID = strings.TrimSpace(id)

	case installer.SourceGitHub:
		repo, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Repo (owner/repo)").
			Show()
		if err != nil {
			return err
		}
		pkg.Repo = strings.TrimSpace(repo)

		ver, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Version").
			WithDefaultValue("latest").
			Show()
		if err != nil {
			return err
		}
		pkg.Version = strings.TrimSpace(ver)

	case installer.SourceThirdParty:
		u, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Download URL").
			Show()
		if err != nil {
			return err
		}
		pkg.URL = strings.TrimSpace(u)
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
func packageIDLine(pkg config.Package) string {
	switch pkg.Source {
	case installer.SourceWinget, installer.SourceChocolatey, installer.SourceScoop:
		if pkg.ID != "" {
			return fmt.Sprintf("id: %s", pkg.ID)
		}
	case installer.SourceGitHub:
		if pkg.Repo != "" {
			line := fmt.Sprintf("repo: %s", pkg.Repo)
			if pkg.Version != "" {
				line += fmt.Sprintf("  @%s", pkg.Version)
			}
			return line
		}
	case installer.SourceThirdParty:
		if pkg.URL != "" {
			return fmt.Sprintf("url: %s", pkg.URL)
		}
	}
	return ""
}

// writeWizardResult writes the collected packages to path as a YAML file.
func writeWizardResult(path string, entries []wizardPkg, catOrder []string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()

	fmt.Fprintf(f, "# Generated by apptide init on %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(f, "# Run: apptide install\n\n")

	// Group entries by category, preserving catOrder.
	groups := make(map[string][]config.Package)
	for _, e := range entries {
		groups[e.category] = append(groups[e.category], e.pkg)
	}

	for _, cat := range catOrder {
		pkgs, ok := groups[cat]
		if !ok || len(pkgs) == 0 {
			continue
		}
		fmt.Fprintf(f, "%s:\n", cat)
		for _, pkg := range pkgs {
			writePackageEntry(f, pkg)
		}
	}

	// Packages whose category wasn't in catOrder (shouldn't happen, but be safe).
	written := make(map[string]bool)
	for _, c := range catOrder {
		written[c] = true
	}
	for _, e := range entries {
		if !written[e.category] {
			fmt.Fprintf(f, "%s:\n", e.category)
			writePackageEntry(f, e.pkg)
			written[e.category] = true
		}
	}

	return nil
}

// ── Templates ─────────────────────────────────────────────────────────────────

func runInitTemplate() error {
	var content string
	switch initTemplate {
	case "example":
		content = templateExample()
	case "minimal", "":
		content = templateMinimal()
	default:
		return fmt.Errorf("unknown template %q — valid: minimal, example", initTemplate)
	}

	if err := os.WriteFile(initOutputFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", initOutputFile, err)
	}

	pterm.Success.Printfln("%s created (template: %s)", pterm.Cyan(initOutputFile), initTemplate)
	pterm.Info.Println("Edit the file, then run: apptide install")
	return nil
}

func templateMinimal() string {
	return fmt.Sprintf(`# apptide packages config — generated %s
# Docs: apptide --help
#
# Fields reference:
#   name          : display name (required)
#   source        : winget | chocolatey | scoop | github | third_party (required)
#   action        : install | uninstall | skip  (default: install)
#   description   : informational
#   no_upgrade    : true → skip upgrade when already installed
#   post_install  : shell command to run after install (e.g. "git config ...")
#
#   winget / chocolatey / scoop → id: "Publisher.App"
#   github                      → repo: "owner/repo"  version: "latest"
#   third_party                 → url: "https://..."  run_installer: true

MyApps:
  - name: "Example App"
    source: winget
    id: "Publisher.AppId"
    description: "Replace this with your first package"
    action: install
`, time.Now().Format("2006-01-02"))
}

func templateExample() string {
	return fmt.Sprintf(`# apptide packages config — generated %s
# One package per source type. Edit freely, then run: apptide install

Development:
  - name: "Git"
    source: winget
    id: "Git.Git"
    description: "Distributed version control system"
    action: install

  - name: "Vim"
    source: scoop
    id: "vim"
    description: "Highly configurable text editor"
    action: install

  - name: "Clink"
    source: chocolatey
    id: "clink"
    description: "Powerful readline editing for cmd.exe"
    action: install

  - name: "Lazygit"
    source: github
    repo: "jesseduffield/lazygit"
    version: latest
    description: "Terminal UI for git"
    action: install

Utilities:
  - name: "7-Zip"
    source: winget
    id: "7zip.7zip"
    description: "High-compression file archiver"
    action: install

  - name: "jq"
    source: winget
    id: "jqlang.jq"
    description: "Command-line JSON processor"
    action: install
`, time.Now().Format("2006-01-02"))
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// writePackageEntry writes a single YAML package block to w.
func writePackageEntry(w *os.File, pkg config.Package) {
	fmt.Fprintf(w, "  - name: %q\n", pkg.Name)
	fmt.Fprintf(w, "    source: %s\n", pkg.Source)

	switch pkg.Source {
	case installer.SourceWinget, installer.SourceChocolatey, installer.SourceScoop:
		if pkg.ID != "" {
			fmt.Fprintf(w, "    id: %q\n", pkg.ID)
		}
	case installer.SourceGitHub:
		if pkg.Repo != "" {
			fmt.Fprintf(w, "    repo: %q\n", pkg.Repo)
		}
		v := pkg.Version
		if v == "" {
			v = "latest"
		}
		fmt.Fprintf(w, "    version: %q\n", v)
	case installer.SourceThirdParty:
		if pkg.URL != "" {
			fmt.Fprintf(w, "    url: %q\n", pkg.URL)
		}
	}

	if pkg.Description != "" {
		fmt.Fprintf(w, "    description: %q\n", pkg.Description)
	}
	fmt.Fprintf(w, "    action: %s\n\n", pkg.Action)
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
