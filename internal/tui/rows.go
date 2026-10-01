package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/inventory"
	"github.com/lucasassuncao/apptide/internal/runner"
	"github.com/lucasassuncao/apptide/internal/state"
)

// rowState is what the glyph column encodes.
type rowState uint8

const (
	stateInstalled rowState = iota
	stateMissing
	stateConflict
	stateSkip
	stateUnavailable // the manager that owns it is not on this machine
	stateNoSource
	stateAdopted // installed but absent from the config (Installed tab)
	stateTracked // installed and present in the config (Installed tab)
	stateBroken  // the manager records the install as failed
)

func (s rowState) glyph() string {
	switch s {
	case stateInstalled, stateTracked:
		return stGreen.Render("●")
	case stateMissing:
		return stDim.Render("○")
	case stateConflict:
		return stRed.Render("!")
	case stateSkip:
		return stDim.Render("–")
	case stateUnavailable, stateNoSource:
		return stYellow.Render("⚠")
	case stateBroken:
		return stRed.Render("x")
	case stateAdopted:
		return stYellow.Render("+")
	default:
		return " "
	}
}

func (s rowState) label() string {
	switch s {
	case stateInstalled:
		return stGreen.Render("installed")
	case stateMissing:
		return stDim.Render("missing")
	case stateConflict:
		return stRed.Render("conflict")
	case stateSkip:
		return stDim.Render("skip")
	case stateUnavailable:
		return stYellow.Render("unavailable")
	case stateNoSource:
		return stYellow.Render("no source")
	case stateBroken:
		return stRed.Render("install failed")
	case stateAdopted:
		return stYellow.Render("not in config")
	case stateTracked:
		return stGreen.Render("in config")
	default:
		return ""
	}
}

// row is one line of any list. Which fields are meaningful depends on the tab:
// config-driven tabs carry app and res, the Installed tab carries entry.
type row struct {
	app      config.Application
	res      runner.Resolution
	category string
	version  string
	state    rowState
	selected bool

	// frozen marks an application that will not move on its own: either the
	// version is pinned or upgrades are switched off. Without it a pinned
	// application looks identical to one tracking latest.
	frozen bool

	// available is the newer version a manager offers. On the Upgradable tab it
	// is the point of the row; on the config tabs it is a warning, because an
	// application can be at its declared version and still be behind.
	available string

	// Installed tab only.
	entry      inventory.Entry
	entrySrc   config.Source
	configName string
}

// colSpec is one of the columns that follow Name and Source. Which ones a tab
// shows depends on the question it answers, so they are declared per tab
// rather than squeezed into a fixed set — packing "current → available" into a
// single cell only truncated both halves.
type colSpec struct {
	title string
	value func(row) string
	// cap limits the width; 0 means the title and values decide.
	cap int
}

func columnSpecs(t tab) []colSpec {
	managed := colSpec{
		title: "Managed by apptide",
		value: func(r row) string {
			if r.configName != "" {
				return stGreen.Render("true")
			}
			return stDim.Render("false")
		},
	}
	installed := colSpec{
		title: "Version Installed",
		value: func(r row) string { return r.installedLabel(true) },
	}

	switch t {
	case tabConfig, tabDrift:
		// Declared first: it is the intent the installed value is judged against.
		return []colSpec{
			{title: "Version Declared", value: row.declaredLabel},
			installed,
		}

	case tabUpgradable:
		return []colSpec{
			// No upgrade marker here: every row on this tab has one by
			// definition, and the next column already holds the number.
			{title: "Version Installed", value: func(r row) string { return r.installedLabel(false) }},
			{
				title: "Version Available",
				value: func(r row) string { return stCyan.Render(r.available) },
			},
			managed,
		}

	case tabInstalled:
		return []colSpec{installed, managed}
	}
	return nil
}

// declaredLabel is the version the config asks for. The frozen marker lives
// here because pinning is a property of the declaration, not of the machine.
//
// An absent field and an explicit `version: latest` behave the same but are
// not the same statement, and the column should not make the file look like it
// says something it does not. "none (latest)" reports both: nothing declared,
// and what that means in practice.
func (r row) declaredLabel() string {
	v := r.app.Version

	switch {
	case v == "":
		return stDim.Render("none (latest)")
	case strings.EqualFold(v, "latest"):
		return stDim.Render(v)
	}

	styled := v
	if r.version != "" && r.version != v {
		// The pin and the machine disagree: the next install moves it.
		styled = stYellow.Render(v)
	}
	if r.frozen {
		styled += " ★"
	}
	return styled
}

// installedLabel is the version on the machine. showUpgrade adds a marker when
// a newer release exists — worth showing even for an application sitting
// exactly at its declared version, since "matches the file" and "is current"
// are different statements and only the first is apptide's job.
func (r row) installedLabel(showUpgrade bool) string {
	v := r.version
	if v == "" {
		return stDim.Render("—")
	}
	if showUpgrade && r.available != "" {
		v += " " + stYellow.Render("↑")
	}
	return v
}

// isFrozen reports whether the declaration prevents the application from
// moving: a pinned version, or upgrades disabled.
func isFrozen(app config.Application) bool {
	if app.SkipUpgrade {
		return true
	}
	return app.Version != "" && !strings.EqualFold(app.Version, "latest")
}

func (r row) name() string {
	if r.app.Name != "" {
		return r.app.Name
	}
	return r.entry.ID
}

// sourceLabel renders the source column: which manager owns the application,
// and whether that is the preferred one.
func (r row) sourceLabel() string {
	if r.entrySrc != "" {
		return string(r.entrySrc)
	}

	first, _ := r.app.Source.First()

	switch r.res.Kind {
	case runner.ResolutionConflict:
		names := make([]string, len(r.res.Conflicts))
		for i, s := range r.res.Conflicts {
			names[i] = string(s)
		}
		return stRed.Render(strings.Join(names, "|"))

	case runner.ResolutionBound, runner.ResolutionDetected:
		if r.res.Source != first {
			// Installed by a fallback: say so, since it is not what the
			// preference order alone would suggest.
			return string(r.res.Source) + stYellow.Render(" ⇠")
		}
		return string(r.res.Source)

	default:
		if extra := len(r.app.Source) - 1; extra > 0 {
			return fmt.Sprintf("%s,+%d", first, extra)
		}
		return string(first)
	}
}

// buildConfigRows resolves every application against the snapshot and state.
func buildConfigRows(cfg *config.Config, st *state.State, probe runner.Prober, snap *inventory.Snapshot, upg *inventory.Upgrades) []row {
	rows := make([]row, 0, len(cfg.Applications))

	for _, app := range cfg.Applications {
		r := row{app: app, category: app.CategoryOrDefault(), frozen: isFrozen(app)}

		if app.EffectiveAction() == config.ActionSkip {
			r.state = stateSkip
			r.res = runner.ResolveSource(app, st, probe)
			rows = append(rows, r)
			continue
		}

		r.res = runner.ResolveSource(app, st, probe)

		switch r.res.Kind {
		case runner.ResolutionNoSource:
			r.state = stateNoSource
		case runner.ResolutionConflict:
			r.state = stateConflict
			_, r.version = probe.Probe(r.res.Source, app)
		default:
			installed, version := probe.Probe(r.res.Source, app)
			r.version = version
			switch {
			case installed:
				r.state = stateInstalled
			case r.res.Source.Normalize() != config.SourceGitHub && !snap.Available(r.res.Source):
				// The owning manager is missing from this machine, so "not
				// installed" would be a guess rather than a fact.
				r.state = stateUnavailable
			default:
				r.state = stateMissing
			}
		}

		if r.state == stateInstalled && upg != nil {
			if id, ok := app.Package.ID(r.res.Source); ok {
				if u, found := upg.Lookup(r.res.Source, id); found {
					r.available = u.Available
				}
			}
		}

		rows = append(rows, r)
	}
	return rows
}

// buildInstalledRows lists what the machine has, marking which entries the
// config already tracks.
func buildInstalledRows(cfg *config.Config, snap *inventory.Snapshot) []row {
	// Index the config by source+id so each installed package can be matched back.
	type key struct {
		src config.Source
		id  string
	}
	tracked := map[key]string{}
	for _, app := range cfg.Applications {
		for _, src := range app.Package.Configured() {
			if id, ok := app.Package.ID(src); ok && id != "" {
				tracked[key{src.Normalize(), strings.ToLower(id)}] = app.Name
			}
		}
	}

	var rows []row
	for _, src := range snap.Sources() {
		for _, e := range snap.All(src) {
			r := row{entry: e, entrySrc: src, version: e.Version, state: stateAdopted}
			if e.Failed {
				r.state = stateBroken
			}
			if name, ok := tracked[key{src, strings.ToLower(e.ID)}]; ok {
				r.configName = name
				r.state = stateTracked
			}
			rows = append(rows, r)
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].entrySrc != rows[j].entrySrc {
			return rows[i].entrySrc < rows[j].entrySrc
		}
		return strings.ToLower(rows[i].entry.ID) < strings.ToLower(rows[j].entry.ID)
	})
	return rows
}

// buildUpgradableRows lists everything the managers report as outdated,
// grouped by source and matched back to the config where possible.
//
// Packages outside the config are shown too: they are what the machine has,
// and hiding them would answer "what is outdated?" with half the truth. Only
// the ones apptide declares can be upgraded from here, though.
func buildUpgradableRows(cfg *config.Config, upg *inventory.Upgrades) []row {
	if upg == nil {
		return nil
	}

	type key struct {
		src config.Source
		id  string
	}
	declared := map[key]config.Application{}
	for _, app := range cfg.Applications {
		for _, src := range app.Package.Configured() {
			if id, ok := app.Package.ID(src); ok && id != "" {
				declared[key{src.Normalize(), strings.ToLower(id)}] = app
			}
		}
	}

	var rows []row
	for _, src := range upg.Sources() {
		for _, u := range upg.All(src) {
			r := row{
				entry:     inventory.Entry{ID: u.ID, Version: u.Current},
				entrySrc:  src,
				version:   u.Current,
				available: u.Available,
				state:     stateAdopted,
			}
			if app, ok := declared[key{src, strings.ToLower(u.ID)}]; ok {
				r.app = app
				r.category = app.CategoryOrDefault()
				r.configName = app.Name
				r.frozen = isFrozen(app)
				r.state = stateTracked
				if u.Pinned {
					r.frozen = true
				}
			}
			rows = append(rows, r)
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].entrySrc != rows[j].entrySrc {
			return rows[i].entrySrc < rows[j].entrySrc
		}
		return strings.ToLower(rows[i].entry.ID) < strings.ToLower(rows[j].entry.ID)
	})
	return rows
}

// buildDriftRows keeps only the config rows where the machine disagrees with
// the declaration — the actionable subset of the Config tab.
func buildDriftRows(configRows []row) []row {
	var out []row
	for _, r := range configRows {
		switch r.state {
		case stateMissing, stateConflict, stateNoSource:
			out = append(out, r)
		}
	}
	return out
}

// filterRows keeps rows whose name, category or source matches the query.
func filterRows(rows []row, query string) []row {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return rows
	}
	var out []row
	for _, r := range rows {
		hay := strings.ToLower(strings.Join([]string{
			r.name(), r.category, r.app.Description,
			r.app.Source.String(), string(r.entrySrc), strings.Join(r.app.Tags, " "),
		}, " "))
		if strings.Contains(hay, q) {
			out = append(out, r)
		}
	}
	return out
}
