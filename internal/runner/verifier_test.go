package runner

import (
	"encoding/json"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/state"
)

// failingProber fails the test if anything asks it, for paths that must not probe.
type failingProber struct{ t *testing.T }

func (p failingProber) Probe(src config.Source, app config.Application) (bool, string) {
	p.t.Errorf("probed %s for %q", src, app.Name)
	return false, ""
}

// githubApp uses the one source whose installer needs nothing on the machine.
func githubApp(name string) config.Application {
	return config.Application{
		Name:    name,
		Source:  config.Sources{config.SourceGitHub},
		Package: config.Packages{GitHub: &config.GitHubSpec{ID: "owner/" + name}},
	}
}

func TestVerifyOneSkipsWithoutProbing(t *testing.T) {
	app := githubApp("tool")
	app.Action = config.ActionSkip

	got := verifyOne(app, state.New(""), installer.Options{}, failingProber{t})
	if got.status != "skip" {
		t.Errorf("status = %q, want skip", got.status)
	}
}

func TestVerifyOneWithNoSource(t *testing.T) {
	got := verifyOne(config.Application{Name: "Nothing"}, state.New(""), installer.Options{}, failingProber{t})
	if got.status != "no_source" {
		t.Errorf("status = %q, want no_source", got.status)
	}
}

// Two managers claiming the same application is reported, not resolved by
// picking one, and the detail names both.
func TestVerifyOneReportsAConflict(t *testing.T) {
	app := config.Application{
		Name:   "Git",
		Source: config.Sources{config.SourceWinget, config.SourceScoop},
	}
	probe := stubProber{config.SourceWinget: "2.0", config.SourceScoop: "2.1"}

	got := verifyOne(app, state.New(""), installer.Options{}, probe)
	if got.status != "conflict" {
		t.Fatalf("status = %q, want conflict", got.status)
	}
	if got.detail != "installed via winget and scoop" {
		t.Errorf("detail = %q", got.detail)
	}
}

func TestVerifyOneReportsInstalledAndMissing(t *testing.T) {
	st := state.New("")

	got := verifyOne(githubApp("tool"), st, installer.Options{}, stubProber{config.SourceGitHub: "1.4.0"})
	if got.status != "installed" || got.version != "1.4.0" {
		t.Errorf("installed app = (%q, %q), want (installed, 1.4.0)", got.status, got.version)
	}

	got = verifyOne(githubApp("tool"), st, installer.Options{}, stubProber{})
	if got.status != "not_found" {
		t.Errorf("missing app status = %q, want not_found", got.status)
	}
}

// Only not_found counts as missing: a skipped or conflicting application
// must not fail the command.
func TestVerifyJSONFailsOnlyOnMissing(t *testing.T) {
	results := []verifyResult{
		{app: githubApp("a"), category: "Dev", source: config.SourceGitHub, status: "installed", version: "1.0"},
		{app: githubApp("b"), category: "Dev", status: "skip"},
		{app: githubApp("c"), category: "Dev", status: "conflict", detail: "installed via winget and scoop"},
	}

	var err error
	out := captureStdout(t, func() { err = verifyJSON(results) })
	if err != nil {
		t.Errorf("no application is missing, got %v", err)
	}

	var rows []verifyJSONRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not valid JSON: %v\nraw: %s", err, out)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if !rows[0].Installed || rows[0].CurrentVersion != "1.0" || rows[0].ResolvedSource != "github" {
		t.Errorf("installed row = %+v", rows[0])
	}
	if rows[1].Installed || rows[2].Installed {
		t.Error("a skipped or conflicting row was reported as installed")
	}

	results = append(results, verifyResult{app: githubApp("d"), status: "not_found"})
	captureStdout(t, func() { err = verifyJSON(results) })
	if err == nil {
		t.Error("a missing application did not fail verify")
	}
}
