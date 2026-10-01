package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/inventory"
	"github.com/lucasassuncao/apptide/internal/output"
	"github.com/lucasassuncao/apptide/internal/pathutil"
	"github.com/lucasassuncao/apptide/internal/state"
)

// lockTimeout bounds how long we wait for another apptide process to finish
// writing the state file.
const lockTimeout = 10 * time.Second

// Options configures a Runner. Flag values win over the config's settings
// block; empty means "take whatever the file says".
type Options struct {
	ConfigPath  string
	Category    string
	Source      string
	Tags        []string
	DryRun      bool
	Force       bool
	InstallDir  string
	AddToPath   bool
	GitHubToken string
	StatePath   string
}

// applySettings fills in the options the config file provides and the command
// line left empty.
func (o *Options) applySettings(s *config.Settings) {
	if s == nil {
		return
	}
	if o.InstallDir == "" {
		o.InstallDir = s.InstallDir
	}
	if o.GitHubToken == "" {
		o.GitHubToken = s.GitHubToken
	}
	if !o.AddToPath {
		o.AddToPath = s.AddToPath
	}
}

// Runner orchestrates application installation across categories and sources.
type Runner struct {
	opts Options
}

func New(opts Options) *Runner { return &Runner{opts: opts} }

// Run loads the config and processes all matching applications via the TUI.
func (r *Runner) Run() error {
	cfg, err := config.LoadWithImports(r.opts.ConfigPath)
	if err != nil {
		return err
	}

	r.opts.applySettings(cfg.Settings)

	instOpts := installer.Options{
		GitHubToken:       r.opts.GitHubToken,
		DefaultInstallDir: r.opts.InstallDir,
		Force:             r.opts.Force,
	}

	apps, err := selectApps(cfg, r.opts.Category, r.opts.Source, r.opts.Tags)
	if err != nil {
		return err
	}
	if len(apps) == 0 {
		if output.IsJSON() {
			// A run that matched nothing is still a successful run; a consumer
			// must get a parseable document, not a bare line of prose.
			printInstallJSON(nil, tally{}, r.opts.DryRun)
			return nil
		}
		fmt.Println("no applications matched")
		return nil
	}

	rows := make([]tableRow, len(apps))
	for i, app := range apps {
		rows[i] = tableRow{app: app, category: app.CategoryOrDefault()}
	}

	// The state file records which source owns each application. A dry run
	// reads it (to resolve bindings) but never writes.
	statePath := r.opts.StatePath
	if statePath == "" {
		statePath = state.DefaultPath()
	}
	unlock, err := state.Lock(statePath, lockTimeout)
	if err != nil {
		return err
	}
	defer unlock()

	st, quarantined, err := state.LoadOrReset(statePath)
	if err != nil {
		return err
	}
	if quarantined != "" {
		fmt.Fprintf(os.Stderr, "%s state file was unreadable and moved to %s\n",
			lgYellow.Render("warn:"), quarantined)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// One snapshot answers "is this installed?" for every application, instead
	// of a process launch per application per source before anything runs.
	probe := NewInventoryProber(inventory.Installed(ctx), instOpts)

	// JSON mode has no interface to drive, and nothing but the final document
	// may reach stdout; the rows are processed in the same order either way.
	asJSON := output.IsJSON()

	var summary tally
	if asJSON {
		rows = runSequential(ctx, rows, instOpts, r.opts.DryRun, st, probe)
		summary = tallyRows(rows)
	} else {
		m := model{
			rows:     rows,
			instOpts: instOpts,
			dryRun:   r.opts.DryRun,
			state:    st,
			probe:    probe,
			ctx:      ctx,
			cancel:   cancel,
		}

		final, runErr := tea.NewProgram(m, tea.WithOutput(os.Stdout)).Run()
		if runErr != nil {
			return fmt.Errorf("tui: %w", runErr)
		}

		done := final.(model)
		rows = done.rows
		summary = tally{
			Total:             done.nTotal,
			OK:                done.nOK,
			Skipped:           done.nSkip,
			Failed:            done.nFailed,
			FailedRequired:    done.nFailedRequired,
			installedBinaries: done.installedBinaries,
		}
	}

	if !r.opts.DryRun {
		if saveErr := st.Save(); saveErr != nil {
			fmt.Fprintf(os.Stderr, "%s could not save state: %v\n", lgYellow.Render("warn:"), saveErr)
		}
	}

	// PATH management. In JSON mode its progress goes to stderr, so stdout stays
	// a single valid document.
	if r.opts.AddToPath && summary.installedBinaries && !r.opts.DryRun {
		dir := instOpts.DefaultInstallDir
		if dir == "" {
			dir = installer.DefaultInstallDir()
		}
		w := io.Writer(os.Stdout)
		if asJSON {
			w = os.Stderr
		}
		managePathEntry(w, dir)
	}

	if asJSON {
		printInstallJSON(rows, summary, r.opts.DryRun)
	}

	// Optional failures are reported in the table but do not set the exit
	// code: a run of sixty applications should not break a pipeline because of
	// one package already known to be unreliable.
	if summary.FailedRequired > 0 {
		return fmt.Errorf("\n%d application(s) failed", summary.FailedRequired)
	}
	return nil
}

// selectApps applies the --category, --source and --tags filters.
func selectApps(cfg *config.Config, category, source string, tags []string) ([]config.Application, error) {
	apps := cfg.Applications

	if category != "" {
		if !cfg.HasCategory(category) {
			fmt.Fprintf(os.Stderr, "error: category %q not found\navailable: %s\n",
				category, strings.Join(cfg.Categories(), ", "))
			return nil, fmt.Errorf("category %q not found", category)
		}
		apps = cfg.InCategory(category)
	}

	if source != "" {
		src := config.Source(source).Normalize()
		if !src.Valid() {
			return nil, fmt.Errorf("unknown source %q — valid: winget, chocolatey, scoop, github", source)
		}
		filtered := apps[:0:0]
		for _, app := range apps {
			if app.Source.Contains(src) {
				filtered = append(filtered, app)
			}
		}
		apps = filtered
	}

	// Any tag matches, not all: --tags dev,minimal means "either of these",
	// which is how a selection of subsets is usually meant.
	if len(tags) > 0 {
		filtered := apps[:0:0]
		for _, app := range apps {
			if hasAnyTag(app.Tags, tags) {
				filtered = append(filtered, app)
			}
		}
		apps = filtered
	}

	return apps, nil
}

func hasAnyTag(have, want []string) bool {
	for _, w := range want {
		for _, h := range have {
			if strings.EqualFold(h, w) {
				return true
			}
		}
	}
	return false
}

// managePathEntry adds dir to the user PATH, reporting progress to w — stdout
// in table mode, stderr in JSON mode.
func managePathEntry(w io.Writer, dir string) {
	if pathutil.IsInUserPath(dir) {
		fmt.Fprintf(w, "\n%s already in %%PATH%%\n", dir)
		return
	}
	fmt.Fprintf(w, "\nadding %s to user %%PATH%%...\n", dir)
	if err := pathutil.AddToUserPath(dir); err != nil {
		fmt.Fprintf(w, "warn: %v\n", err)
	} else {
		fmt.Fprintln(w, "ok  open a new terminal for PATH changes to take effect")
	}
}
