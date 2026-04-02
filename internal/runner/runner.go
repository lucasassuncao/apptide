package runner

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/pathutil"
)

// ANSI codes — kept for verifier.go and exporter.go.
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
	gray   = "\033[90m"
)

// Options configures a Runner.
type Options struct {
	ConfigPath  string
	Category    string
	Source      string
	DryRun      bool
	InstallDir  string
	AddToPath   bool
	GitHubToken string
}

// Runner orchestrates package installation across categories and sources.
type Runner struct {
	opts Options
}

func New(opts Options) *Runner { return &Runner{opts: opts} }

// Run loads the config and processes all matching packages via the TUI.
func (r *Runner) Run() error {
	cfg, err := config.LoadWithImports(r.opts.ConfigPath)
	if err != nil {
		return err
	}

	instOpts := installer.Options{
		GitHubToken:       r.opts.GitHubToken,
		DefaultInstallDir: r.opts.InstallDir,
	}

	categories := cfg.Categories()
	if r.opts.Category != "" {
		if _, ok := cfg[r.opts.Category]; !ok {
			fmt.Fprintf(os.Stderr, "error: category %q not found\navailable: %s\n",
				r.opts.Category, strings.Join(categories, ", "))
			return fmt.Errorf("category %q not found", r.opts.Category)
		}
		categories = []string{r.opts.Category}
	}

	var rows []tableRow
	for _, cat := range categories {
		pkgs := cfg[cat]
		if r.opts.Source != "" {
			pkgs = filterBySource(pkgs, r.opts.Source)
		}
		for _, pkg := range pkgs {
			rows = append(rows, tableRow{pkg: pkg, category: cat})
		}
	}

	if len(rows) == 0 {
		fmt.Println("no packages matched")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := model{
		rows:     rows,
		instOpts: instOpts,
		dryRun:   r.opts.DryRun,
		ctx:      ctx,
		cancel:   cancel,
	}

	final, err := tea.NewProgram(m, tea.WithOutput(os.Stdout)).Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	done := final.(model)

	// PATH management.
	if r.opts.AddToPath && done.installedBinaries && !r.opts.DryRun {
		dir := instOpts.DefaultInstallDir
		if dir == "" {
			dir = installer.DefaultInstallDir()
		}
		managePathEntry(dir)
	}

	if done.nFailed > 0 {
		return fmt.Errorf("\n%d package(s) failed", done.nFailed)
	}
	return nil
}

func managePathEntry(dir string) {
	if pathutil.IsInUserPath(dir) {
		fmt.Printf("\n%s already in %%PATH%%%s\n", dir, reset)
		return
	}
	fmt.Printf("\nadding %s to user %%PATH%%...\n", dir)
	if err := pathutil.AddToUserPath(dir); err != nil {
		fmt.Printf("warn: %v\n", err)
	} else {
		fmt.Println("ok  open a new terminal for PATH changes to take effect")
	}
}

func filterBySource(pkgs []config.Package, source string) []config.Package {
	out := pkgs[:0:0]
	for _, p := range pkgs {
		if strings.EqualFold(p.Source, source) {
			out = append(out, p)
		}
	}
	return out
}
