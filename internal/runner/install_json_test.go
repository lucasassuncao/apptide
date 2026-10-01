package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/state"
)

func row(name string, status rowStatus) tableRow {
	return tableRow{
		app:      app(name),
		category: "Uncategorized",
		status:   status,
		source:   config.SourceWinget,
	}
}

func TestTallyRowsCountsEachStatusOnce(t *testing.T) {
	rows := []tableRow{
		row("A", statusOK),
		row("B", statusUpToDate),
		row("C", statusAlreadyInstalled),
		row("D", statusSkipped),
		row("E", statusFailed),
		row("F", statusConflict),
	}

	got := tallyRows(rows)
	want := tally{Total: 6, OK: 3, Skipped: 1, Failed: 2, FailedRequired: 2}
	if got != want {
		t.Errorf("tallyRows()\n got: %+v\nwant: %+v", got, want)
	}
}

// An optional failure is reported but must not reach FailedRequired, which is
// what decides the exit code.
func TestTallyRowsExcludesOptionalFromFailedRequired(t *testing.T) {
	optional := row("Flaky", statusFailed)
	optional.app.Optional = true

	rows := []tableRow{row("Solid", statusFailed), optional}

	got := tallyRows(rows)
	if got.Failed != 2 {
		t.Errorf("Failed = %d, want 2", got.Failed)
	}
	if got.FailedRequired != 1 {
		t.Errorf("FailedRequired = %d, want 1 (the optional row must not count)", got.FailedRequired)
	}
}

// PATH management only makes sense when a github binary actually landed.
func TestTallyRowsDetectsInstalledBinaries(t *testing.T) {
	gh := row("Lazygit", statusOK)
	gh.source = config.SourceGitHub

	if !tallyRows([]tableRow{gh}).installedBinaries {
		t.Error("a successful github install should set installedBinaries")
	}
	if tallyRows([]tableRow{row("Git", statusOK)}).installedBinaries {
		t.Error("a winget install should not set installedBinaries")
	}

	// Already installed means nothing new landed on disk.
	upToDate := row("Lazygit", statusUpToDate)
	upToDate.source = config.SourceGitHub
	if tallyRows([]tableRow{upToDate}).installedBinaries {
		t.Error("an up-to-date github row should not set installedBinaries")
	}
}

func TestJSONStatusFollowsTheAction(t *testing.T) {
	tests := []struct {
		name     string
		status   rowStatus
		action   config.Action
		dryRun   bool
		optional bool
		want     string
	}{
		{"install ok", statusOK, config.ActionInstall, false, false, "installed"},
		{"uninstall ok", statusOK, config.ActionUninstall, false, false, "uninstalled"},
		{"dry-run install", statusOK, config.ActionInstall, true, false, "would_install"},
		{"dry-run uninstall", statusOK, config.ActionUninstall, true, false, "would_uninstall"},
		{"up to date", statusUpToDate, config.ActionInstall, false, false, "up_to_date"},
		{"already installed", statusAlreadyInstalled, config.ActionInstall, false, false, "already_installed"},
		{"already uninstalled", statusAlreadyInstalled, config.ActionUninstall, false, false, "already_uninstalled"},
		{"skip", statusSkipped, config.ActionSkip, false, false, "skip"},
		{"conflict", statusConflict, config.ActionInstall, false, false, "conflict"},
		{"failed", statusFailed, config.ActionInstall, false, false, "failed"},
		{"never ran", statusPending, config.ActionInstall, false, false, "pending"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := row("X", tt.status)
			r.app.Action = tt.action
			if got := jsonStatus(r, tt.dryRun); got != tt.want {
				t.Errorf("jsonStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A dry run must never report work it did not do — the row status has to say
// "would", not "installed".
func TestJSONStatusDryRunNeverClaimsSuccess(t *testing.T) {
	r := row("X", statusOK)
	if got := jsonStatus(r, true); got == "installed" {
		t.Errorf("dry run reported %q as if the install happened", got)
	}
}

// captureStdout runs fn with os.Stdout replaced by a pipe and returns what it
// wrote. output.PrintJSON writes straight to os.Stdout, which is the behaviour
// under test: in JSON mode nothing else may reach it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	w.Close()
	return <-done
}

// decodeInstallJSON captures stdout, runs printInstallJSON, and decodes it.
func decodeInstallJSON(t *testing.T, rows []tableRow, tl tally, dryRun bool) installJSONDoc {
	t.Helper()

	out := captureStdout(t, func() { printInstallJSON(rows, tl, dryRun) })

	var doc installJSONDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out)
	}
	return doc
}

func TestPrintInstallJSONShape(t *testing.T) {
	ok := row("Git", statusOK)
	ok.newVersion = "2.51.0"
	ok.prevVersion = "2.50.0"

	fallback := row("Lazygit", statusOK)
	fallback.app.Source = config.Sources{config.SourceWinget, config.SourceScoop}
	fallback.source = config.SourceScoop
	fallback.viaFallback = true
	fallback.newVersion = "0.42.0"

	failed := row("Broken", statusFailed)
	failed.app.Optional = true
	failed.detail = "choco not found in PATH"

	rows := []tableRow{ok, fallback, failed}
	doc := decodeInstallJSON(t, rows, tallyRows(rows), false)

	if doc.DryRun {
		t.Error("dry_run should be false")
	}
	if len(doc.Applications) != 3 {
		t.Fatalf("got %d applications, want 3", len(doc.Applications))
	}

	first := doc.Applications[0]
	if first.Name != "Git" || first.Status != "installed" {
		t.Errorf("first row = %+v", first)
	}
	if first.Version != "2.51.0" || first.PreviousVersion != "2.50.0" {
		t.Errorf("version fields lost: %+v", first)
	}

	second := doc.Applications[1]
	if second.ResolvedSource != "scoop" || !second.ViaFallback {
		t.Errorf("fallback not reported: %+v", second)
	}
	if len(second.Sources) != 2 || second.Sources[0] != "winget" {
		t.Errorf("preference list not reported in order: %+v", second.Sources)
	}

	third := doc.Applications[2]
	if third.Status != "failed" || !third.Optional || third.Detail == "" {
		t.Errorf("failure detail lost: %+v", third)
	}

	if doc.Summary.Total != 3 || doc.Summary.OK != 2 || doc.Summary.Failed != 1 {
		t.Errorf("summary = %+v", doc.Summary)
	}
	// The one failure was optional, so the run should still exit zero.
	if doc.Summary.FailedRequired != 0 {
		t.Errorf("failed_required = %d, want 0", doc.Summary.FailedRequired)
	}
}

// A run that matched nothing is still a successful run, and a consumer must get
// a parseable document rather than a line of prose.
func TestPrintInstallJSONWithNoRows(t *testing.T) {
	doc := decodeInstallJSON(t, nil, tally{}, false)

	if doc.Applications == nil {
		t.Error("applications should be an empty array, not null")
	}
	if len(doc.Applications) != 0 || doc.Summary.Total != 0 {
		t.Errorf("expected an empty document, got %+v", doc)
	}
}

func TestPrintInstallJSONMarksDryRun(t *testing.T) {
	rows := []tableRow{row("Git", statusOK)}
	doc := decodeInstallJSON(t, rows, tallyRows(rows), true)

	if !doc.DryRun {
		t.Error("dry_run should be true")
	}
	if got := doc.Applications[0].Status; got != "would_install" {
		t.Errorf("status = %q, want would_install", got)
	}
}

// Skip rows never touch a package manager, so this exercises the loop itself
// without shelling out.
func TestRunSequentialProcessesEveryRow(t *testing.T) {
	rows := make([]tableRow, 3)
	for i, name := range []string{"A", "B", "C"} {
		a := app(name)
		a.Action = config.ActionSkip
		rows[i] = tableRow{app: a, category: "Uncategorized"}
	}

	got := runSequential(context.Background(), rows, installer.Options{}, false, state.New(""), installerProber{})

	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3", len(got))
	}
	for i, r := range got {
		if r.status != statusSkipped {
			t.Errorf("row %d: status = %v, want statusSkipped", i, r.status)
		}
	}
	if tl := tallyRows(got); tl.Skipped != 3 || tl.Total != 3 {
		t.Errorf("tally = %+v", tl)
	}
}

// A cancelled context must not leave the remaining rows looking like successes.
func TestRunSequentialStopsOnCancel(t *testing.T) {
	rows := make([]tableRow, 2)
	for i, name := range []string{"A", "B"} {
		a := app(name)
		a.Action = config.ActionSkip
		rows[i] = tableRow{app: a, category: "Uncategorized"}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := runSequential(ctx, rows, installer.Options{}, false, state.New(""), installerProber{})
	for i, r := range got {
		if r.status != statusFailed || r.detail != "cancelled" {
			t.Errorf("row %d: got (%v, %q), want (statusFailed, \"cancelled\")", i, r.status, r.detail)
		}
	}
	if tl := tallyRows(got); tl.FailedRequired != 2 {
		t.Errorf("cancelled rows should count as required failures, got %+v", tl)
	}
}
