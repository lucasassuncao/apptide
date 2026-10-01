package cmd

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/output"
	"github.com/lucasassuncao/apptide/internal/state"
	"github.com/lucasassuncao/bezel/table"
	"github.com/spf13/cobra"
)

var (
	adoptSource string
	adoptForget bool
	adoptList   bool
)

var adoptCmd = &cobra.Command{
	Use:   "adopt [application]",
	Short: "Record which source manages an application",
	Long: `Bind an application to a package source in the local state file.

apptide never changes this binding on its own: the preference order in
source: applies to the first installation only, and re-deciding it later would
mean uninstalling and reinstalling software because a catalog changed. This
command is the explicit way to change it.

It only rewrites the binding — nothing is installed or removed. Use it to
resolve a conflict (the same application installed through two managers), or
after moving an application between managers by hand.

The application is named by its config 'name:' field. Names are normalized the
same way the state file keys them — lowercased, with runs of whitespace joined
by a hyphen — so "VS Code", "vs code" and "vs-code" all refer to the same
entry. Use --list to see the recorded names.

This is also the only view of the state file: --list shows what apptide
believes it installed, which is not the same question as what the config
declares ('apptide list') or what the machine has ('apptide verify').`,
	Example: `  # Show every application apptide tracks, and which source owns it
  apptide adopt --list
  apptide adopt --list --output json

  # Two managers report Neovim as installed — declare which one owns it
  apptide adopt Neovim --source scoop

  # Stop tracking an application entirely
  apptide adopt Neovim --forget`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if adoptList {
			if len(args) > 0 {
				return fmt.Errorf("--list takes no arguments (got %q)", args[0])
			}
			return runAdoptList()
		}

		if len(args) != 1 {
			return errors.New("an application name is required — run 'apptide adopt --list' to see the recorded names")
		}
		name := args[0]

		statePath := state.DefaultPath()
		unlock, err := state.Lock(statePath, lockWait)
		if err != nil {
			return err
		}
		defer unlock()

		st, err := state.Load(statePath)
		if err != nil {
			return err
		}

		if adoptForget {
			if !st.Remove(name) {
				return fmt.Errorf("no state entry for %q — run 'apptide adopt --list' to see the recorded names", name)
			}
			if err := st.Save(); err != nil {
				return err
			}
			fmt.Printf("%q is no longer tracked\n", name)
			return nil
		}

		src := config.Source(adoptSource).Normalize()
		if !src.Valid() {
			return fmt.Errorf("unknown source %q — valid: winget, chocolatey, scoop, github", adoptSource)
		}

		// The config is consulted only to validate the pairing and to learn the
		// package id for a first binding; adopt still works on a machine whose
		// config has moved or gone.
		app, haveApp := findConfigApp(configPath, name)
		if haveApp && !app.Source.Contains(src) {
			return fmt.Errorf(
				"%q does not list %q in its source: (%s) — binding it there would make install fail",
				app.Name, src, app.Source)
		}

		if err := st.Adopt(name, string(src)); err != nil {
			// No entry yet. This is the case the conflict message from install
			// leads to: two managers report the application, so nothing is bound
			// and there is nothing to rewrite. Record the choice instead.
			if !errors.Is(err, state.ErrNotFound) {
				return err
			}
			if !haveApp {
				return fmt.Errorf(
					"%q is neither tracked nor declared in %s — check the name with 'apptide list'",
					name, configPath)
			}
			id, _ := app.Package.ID(src)
			st.Bind(app.Name, state.Entry{InstalledVia: string(src), PackageID: id})
		}

		if err := st.Save(); err != nil {
			return err
		}

		fmt.Printf("%q is now managed by %s\n", name, src)
		return nil
	},
}

// findConfigApp returns the configured application whose name matches, comparing
// normalized keys so the argument may be written in any of the accepted forms.
func findConfigApp(path, name string) (config.Application, bool) {
	cfg, err := config.LoadWithImports(path)
	if err != nil {
		return config.Application{}, false
	}
	want := state.Key(name)
	for _, app := range cfg.Applications {
		if state.Key(app.Name) == want {
			return app, true
		}
	}
	return config.Application{}, false
}

// adoptListRow is one tracked application in `adopt --list`.
type adoptListRow struct {
	Name         string    `json:"name"`
	InstalledVia string    `json:"installed_via"`
	PackageID    string    `json:"package_id,omitempty"`
	Version      string    `json:"version,omitempty"`
	InstallDir   string    `json:"install_dir,omitempty"`
	Binary       string    `json:"binary,omitempty"`
	InstalledAt  time.Time `json:"installed_at"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

// runAdoptList prints the state file.
//
// No lock is taken: this reads and never writes, Save is atomic (temp file plus
// rename), and blocking a listing behind a sixty-package install would buy
// nothing.
func runAdoptList() error {
	statePath := state.DefaultPath()

	st, err := state.Load(statePath)
	if err != nil {
		return err
	}

	keys := st.Keys()
	rows := make([]adoptListRow, 0, len(keys))
	for _, key := range keys {
		e, ok := st.Get(key)
		if !ok {
			continue
		}
		rows = append(rows, adoptListRow{
			Name:         key,
			InstalledVia: e.InstalledVia,
			PackageID:    e.PackageID,
			Version:      e.Version,
			InstallDir:   e.InstallDir,
			Binary:       e.Binary,
			InstalledAt:  e.InstalledAt,
			UpdatedAt:    e.UpdatedAt,
		})
	}

	if output.IsJSON() {
		output.PrintJSON(rows)
		return nil
	}
	printAdoptList(rows, statePath)
	return nil
}

var adoptListCols = []table.Column{
	{Title: "Application"}, {Title: "Source"}, {Title: "Version"}, {Title: "Package ID"}, {Title: "Updated"},
}

func printAdoptList(rows []adoptListRow, statePath string) {
	if len(rows) == 0 {
		fmt.Printf("\n%s — %s\n\n", th.Dim.Render("no applications tracked yet"), statePath)
		return
	}

	cells := make([][]string, len(rows))
	for i, r := range rows {
		cells[i] = []string{
			r.Name,
			th.Info.Render(dash(r.InstalledVia)),
			dash(r.Version),
			th.Dim.Render(dash(r.PackageID)),
			th.Dim.Render(stamp(r.UpdatedAt, r.InstalledAt)),
		}
	}
	widths := table.Fit(adoptListCols, cells, math.MaxInt)

	fmt.Println("\n  " + th.Bold.Render(table.Titles(adoptListCols, widths)))
	fmt.Println(rule(2, widths))
	for _, c := range cells {
		fmt.Println("  " + table.Row(widths, c))
	}

	fmt.Printf("\n%s tracked in %s\n\n",
		th.Warning.Render(fmt.Sprintf("%d application(s)", len(rows))), th.Dim.Render(statePath))
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// stamp shows when the binding last changed, falling back to when it was first
// recorded — UpdatedAt is only set once an entry has been rewritten.
func stamp(updated, installed time.Time) string {
	t := updated
	if t.IsZero() {
		t = installed
	}
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// lockWait bounds how long adopt waits for another apptide process to release
// the state file.
const lockWait = 10 * time.Second

func init() {
	rootCmd.AddCommand(adoptCmd)
	adoptCmd.Flags().StringVarP(&adoptSource, "source", "s", "", "source that manages this application (required unless --forget or --list)")
	adoptCmd.Flags().BoolVar(&adoptForget, "forget", false, "remove the application from the state file")
	adoptCmd.Flags().BoolVar(&adoptList, "list", false, "list every tracked application and the source that owns it")

	adoptCmd.MarkFlagsMutuallyExclusive("list", "forget")
	adoptCmd.MarkFlagsMutuallyExclusive("list", "source")
	adoptCmd.MarkFlagsMutuallyExclusive("forget", "source")
}
