package runner

import (
	"context"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/output"
	"github.com/lucasassuncao/apptide/internal/state"
)

// runSequential processes every row in order without a TUI, and returns the
// rows with their results filled in.
//
// The bubbletea program is what drives the rows in table mode; with --output
// json there is no interface to drive, and nothing but the final document may
// reach stdout. The per-row work itself is identical — both paths call
// doInstall.
func runSequential(
	ctx context.Context,
	rows []tableRow,
	opts installer.Options,
	dryRun bool,
	st *state.State,
	probe Prober,
) []tableRow {
	for i := range rows {
		if ctx.Err() != nil {
			// Cancelled mid-run: the rows already done keep their results, and
			// the rest are reported as such rather than as successes.
			rows[i].status = statusFailed
			rows[i].detail = "cancelled"
			continue
		}
		applyResult(&rows[i], doInstall(ctx, i, rows[i], opts, dryRun, st, probe))
	}
	return rows
}

// tally is the run-level count of what happened.
type tally struct {
	Total   int `json:"total"`
	OK      int `json:"ok"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
	// FailedRequired excludes applications marked optional, and is what decides
	// the exit code.
	FailedRequired int `json:"failed_required"`

	// installedBinaries reports whether any github binary landed on disk, which
	// is what makes PATH management worth doing. Unexported: it is a decision
	// input, not part of the reported summary.
	installedBinaries bool
}

// tallyRows summarizes finished rows. The TUI keeps its own running counters as
// it draws; this derives the same numbers after the fact.
func tallyRows(rows []tableRow) tally {
	var t tally
	for _, r := range rows {
		t.Total++
		switch r.status {
		case statusOK, statusUpToDate, statusAlreadyInstalled:
			t.OK++
			if r.status == statusOK && r.source == config.SourceGitHub {
				t.installedBinaries = true
			}
		case statusSkipped:
			t.Skipped++
		case statusFailed, statusConflict:
			t.Failed++
			if !r.app.Optional {
				t.FailedRequired++
			}
		case statusPending:
			// A row that never ran; counted in Total only.
		}
	}
	return t
}

// installJSONRow is one application in the `install --output json` document.
type installJSONRow struct {
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Sources  []string `json:"sources"`
	// ResolvedSource is the manager that actually handled the row.
	ResolvedSource string `json:"resolved_source,omitempty"`
	// ViaFallback is set when ResolvedSource was not the first preference.
	ViaFallback     bool   `json:"via_fallback,omitempty"`
	Action          string `json:"action"`
	Status          string `json:"status"`
	PreviousVersion string `json:"previous_version,omitempty"`
	Version         string `json:"version,omitempty"`
	// Optional marks a row whose failure did not affect the exit code.
	Optional bool   `json:"optional,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// installJSONDoc is the document `install --output json` writes to stdout.
//
// Unlike list and verify, which emit a bare array, install is an action: a CI
// job needs the run-level facts (was this a dry run, how many failed) without
// having to re-derive them from the rows.
type installJSONDoc struct {
	DryRun       bool             `json:"dry_run"`
	Applications []installJSONRow `json:"applications"`
	Summary      tally            `json:"summary"`
}

// jsonStatus maps a row status to a stable string for scripts.
//
// The wording follows the action, because "installed" on an uninstall row would
// be actively misleading, and a dry run must never claim work it did not do.
func jsonStatus(r tableRow, dryRun bool) string {
	uninstalling := r.app.EffectiveAction() == config.ActionUninstall

	switch r.status {
	case statusOK:
		switch {
		case dryRun && uninstalling:
			return "would_uninstall"
		case dryRun:
			return "would_install"
		case uninstalling:
			return "uninstalled"
		default:
			return "installed"
		}
	case statusUpToDate:
		return "up_to_date"
	case statusAlreadyInstalled:
		if uninstalling {
			return "already_uninstalled"
		}
		return "already_installed"
	case statusSkipped:
		return "skip"
	case statusConflict:
		return "conflict"
	case statusFailed:
		return "failed"
	default:
		return "pending"
	}
}

// printInstallJSON writes the run document to stdout.
func printInstallJSON(rows []tableRow, t tally, dryRun bool) {
	out := installJSONDoc{
		DryRun:       dryRun,
		Applications: make([]installJSONRow, 0, len(rows)),
		Summary:      t,
	}

	for _, r := range rows {
		sources := make([]string, len(r.app.Source))
		for i, s := range r.app.Source {
			sources[i] = string(s)
		}
		out.Applications = append(out.Applications, installJSONRow{
			Name:            r.app.Name,
			Category:        r.category,
			Sources:         sources,
			ResolvedSource:  string(r.source),
			ViaFallback:     r.viaFallback,
			Action:          string(r.app.EffectiveAction()),
			Status:          jsonStatus(r, dryRun),
			PreviousVersion: r.prevVersion,
			Version:         r.newVersion,
			Optional:        r.app.Optional,
			Detail:          r.detail,
		})
	}

	output.PrintJSON(out)
}
