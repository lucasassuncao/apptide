package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/output"
	"github.com/lucasassuncao/apptide/internal/state"
)

// adoptEnv points state.DefaultPath() at a temp directory and resets the
// command's package-level flags, so each case starts from a known state.
func adoptEnv(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("LOCALAPPDATA", home)

	// state.Lock creates a sibling file, so the directory has to exist.
	dir := filepath.Join(home, "apptide")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	prevSource, prevForget, prevList, prevConfig := adoptSource, adoptForget, adoptList, configPath
	t.Cleanup(func() {
		adoptSource, adoptForget, adoptList, configPath = prevSource, prevForget, prevList, prevConfig
		output.Set("table")
	})
	adoptSource, adoptForget, adoptList = "", false, false

	return filepath.Join(dir, "state.json")
}

// writeAdoptConfig writes a config declaring one application and points
// configPath at it.
func writeAdoptConfig(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "packages.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath = path
}

const neovimConfig = `schema_version: 2
applications:
  - name: "Neovim"
    source: [winget, scoop]
    package:
      winget:
        id: Neovim.Neovim
      scoop:
        id: neovim
`

func runAdopt(t *testing.T, args ...string) error {
	t.Helper()
	return adoptCmd.RunE(adoptCmd, args)
}

func loadAdoptState(t *testing.T, path string) *state.State {
	t.Helper()
	st, err := state.Load(path)
	if err != nil {
		t.Fatalf("loading state: %v", err)
	}
	return st
}

// The conflict message from install leads here: two managers report the
// application, so nothing is bound and there is no entry to rewrite. Before the
// Bind fallback this failed with "no state entry", making the advertised
// remediation impossible.
func TestAdoptCreatesBindingWhenNothingIsTracked(t *testing.T) {
	statePath := adoptEnv(t)
	writeAdoptConfig(t, neovimConfig)
	adoptSource = "scoop"

	if err := runAdopt(t, "Neovim"); err != nil {
		t.Fatalf("adopt on an untracked application: %v", err)
	}

	e, ok := loadAdoptState(t, statePath).Get("Neovim")
	if !ok {
		t.Fatal("no entry was recorded")
	}
	if e.InstalledVia != "scoop" {
		t.Errorf("installed_via = %q, want scoop", e.InstalledVia)
	}
	// The package id comes from the config block of the chosen source, so the
	// entry is usable for upgrades and removal.
	if e.PackageID != "neovim" {
		t.Errorf("package_id = %q, want neovim", e.PackageID)
	}
}

func TestAdoptRewritesAnExistingBinding(t *testing.T) {
	statePath := adoptEnv(t)
	writeAdoptConfig(t, neovimConfig)

	st := state.New(statePath)
	st.Bind("Neovim", state.Entry{InstalledVia: "winget", PackageID: "Neovim.Neovim"})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	adoptSource = "scoop"
	if err := runAdopt(t, "Neovim"); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	e, _ := loadAdoptState(t, statePath).Get("Neovim")
	if e.InstalledVia != "scoop" {
		t.Errorf("installed_via = %q, want scoop", e.InstalledVia)
	}
}

// The argument is the config 'name:', normalized the way the state file keys it.
func TestAdoptAcceptsEveryNameForm(t *testing.T) {
	for _, name := range []string{"VS Code", "vs code", "vs-code", "VS   CODE"} {
		t.Run(name, func(t *testing.T) {
			statePath := adoptEnv(t)
			writeAdoptConfig(t, `schema_version: 2
applications:
  - name: "VS Code"
    source: winget
    package:
      winget:
        id: Microsoft.VisualStudioCode
`)
			adoptSource = "winget"

			if err := runAdopt(t, name); err != nil {
				t.Fatalf("adopt %q: %v", name, err)
			}
			if _, ok := loadAdoptState(t, statePath).Get("vs-code"); !ok {
				t.Errorf("adopt %q did not reach the vs-code entry", name)
			}
		})
	}
}

// Binding an application to a source its config does not list would make the
// next install fail with "not offered", far from the cause.
func TestAdoptRejectsSourceAbsentFromConfig(t *testing.T) {
	adoptEnv(t)
	writeAdoptConfig(t, neovimConfig)
	adoptSource = "chocolatey"

	err := runAdopt(t, "Neovim")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "does not list") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestAdoptRejectsUnknownSource(t *testing.T) {
	adoptEnv(t)
	writeAdoptConfig(t, neovimConfig)
	adoptSource = "apt"

	if err := runAdopt(t, "Neovim"); err == nil {
		t.Fatal("expected an error for an unknown source")
	}
}

func TestAdoptRejectsNameInNeitherStateNorConfig(t *testing.T) {
	adoptEnv(t)
	writeAdoptConfig(t, neovimConfig)
	adoptSource = "winget"

	err := runAdopt(t, "Ghost")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "neither tracked nor declared") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestAdoptForget(t *testing.T) {
	statePath := adoptEnv(t)

	st := state.New(statePath)
	st.Bind("Neovim", state.Entry{InstalledVia: "scoop"})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	adoptForget = true
	if err := runAdopt(t, "Neovim"); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if _, ok := loadAdoptState(t, statePath).Get("Neovim"); ok {
		t.Error("entry survived --forget")
	}

	// Forgetting something untracked is an error, not a silent no-op.
	if err := runAdopt(t, "Neovim"); err == nil {
		t.Error("expected an error forgetting an untracked application")
	}
}

func TestAdoptListRejectsAnArgument(t *testing.T) {
	adoptEnv(t)
	adoptList = true

	if err := runAdopt(t, "Neovim"); err == nil {
		t.Fatal("--list should reject a positional argument")
	}
}

func TestAdoptListJSON(t *testing.T) {
	statePath := adoptEnv(t)

	st := state.New(statePath)
	st.Bind("VS Code", state.Entry{InstalledVia: "winget", PackageID: "Microsoft.VisualStudioCode", Version: "1.99"})
	st.Bind("ack", state.Entry{InstalledVia: "scoop", PackageID: "ack"})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	adoptList = true
	output.Set("json")

	out := captureCmdStdout(t, func() {
		if err := runAdopt(t); err != nil {
			t.Fatalf("adopt --list: %v", err)
		}
	})

	var rows []adoptListRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not valid JSON: %v\nraw: %s", err, out)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	// Keys() sorts, so "ack" precedes "vs-code".
	if rows[0].Name != "ack" || rows[1].Name != "vs-code" {
		t.Errorf("rows are not sorted by key: %q, %q", rows[0].Name, rows[1].Name)
	}
	// The normalized key is what --list reports, because that is what the state
	// holds and what adopt matches against.
	if rows[1].InstalledVia != "winget" || rows[1].PackageID != "Microsoft.VisualStudioCode" {
		t.Errorf("entry fields lost: %+v", rows[1])
	}
	if rows[1].InstalledAt.IsZero() {
		t.Error("installed_at should be set by Bind")
	}
}

// An empty state must still produce a parseable document.
func TestAdoptListJSONWithEmptyState(t *testing.T) {
	adoptEnv(t)
	adoptList = true
	output.Set("json")

	out := captureCmdStdout(t, func() {
		if err := runAdopt(t); err != nil {
			t.Fatalf("adopt --list: %v", err)
		}
	})

	var rows []adoptListRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not valid JSON: %v\nraw: %s", err, out)
	}
	if len(rows) != 0 {
		t.Errorf("want an empty array, got %d rows", len(rows))
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Errorf("empty state should emit [], got %q", out)
	}
}

func TestAdoptListTable(t *testing.T) {
	statePath := adoptEnv(t)

	st := state.New(statePath)
	st.Bind("VS Code", state.Entry{InstalledVia: "winget", PackageID: "Microsoft.VisualStudioCode"})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	adoptList = true
	out := captureCmdStdout(t, func() {
		if err := runAdopt(t); err != nil {
			t.Fatalf("adopt --list: %v", err)
		}
	})

	for _, want := range []string{"vs-code", "winget", "Microsoft.VisualStudioCode", "1 application(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestAdoptListTableWithEmptyState(t *testing.T) {
	adoptEnv(t)
	adoptList = true

	out := captureCmdStdout(t, func() {
		if err := runAdopt(t); err != nil {
			t.Fatalf("adopt --list: %v", err)
		}
	})
	if !strings.Contains(out, "no applications tracked") {
		t.Errorf("expected an empty-state notice, got:\n%s", out)
	}
}

func TestFindConfigAppMissingConfigIsNotAnError(t *testing.T) {
	configPath = filepath.Join(t.TempDir(), "does-not-exist.yaml")
	t.Cleanup(func() { configPath = "" })

	if _, ok := findConfigApp(configPath, "Neovim"); ok {
		t.Error("a missing config should report no application, not a match")
	}
}

// adopt must keep working when the config has moved or gone, as long as the
// binding already exists and is only being rewritten.
func TestAdoptRewritesWithoutAConfig(t *testing.T) {
	statePath := adoptEnv(t)
	configPath = filepath.Join(t.TempDir(), "gone.yaml")

	st := state.New(statePath)
	st.Bind("Neovim", state.Entry{InstalledVia: "winget"})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	adoptSource = "scoop"
	if err := runAdopt(t, "Neovim"); err != nil {
		t.Fatalf("adopt without a config: %v", err)
	}

	e, _ := loadAdoptState(t, statePath).Get("Neovim")
	if e.InstalledVia != "scoop" {
		t.Errorf("installed_via = %q, want scoop", e.InstalledVia)
	}
}

func TestAdoptSourceNormalizesChocoAlias(t *testing.T) {
	statePath := adoptEnv(t)
	writeAdoptConfig(t, `schema_version: 2
applications:
  - name: "Clink"
    source: chocolatey
    package:
      chocolatey:
        id: clink
`)
	adoptSource = "choco"

	if err := runAdopt(t, "Clink"); err != nil {
		t.Fatalf("adopt with the choco alias: %v", err)
	}
	e, _ := loadAdoptState(t, statePath).Get("Clink")
	if e.InstalledVia != string(config.SourceChocolatey) {
		t.Errorf("installed_via = %q, want chocolatey", e.InstalledVia)
	}
}
