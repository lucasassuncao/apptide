package tui

import (
	"fmt"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/inventory"
	"github.com/lucasassuncao/apptide/internal/runner"
	"github.com/lucasassuncao/apptide/internal/state"

	"github.com/lucasassuncao/bezel/draw"
)

// renderSourceChain lists the declared sources in preference order with what
// each one currently answers, so the reason a fallback happened is visible
// rather than inferred from the outcome.
func renderSourceChain(r row, snap *inventory.Snapshot) []string {
	app := r.app
	if len(app.Source) == 0 {
		return nil
	}

	out := []string{stDim.Render("Sources")}
	for i, src := range app.Source {
		mark, note := sourceStatus(app, src, snap)
		if src == r.res.Source && r.res.Kind != runner.ResolutionFresh {
			note += "  " + stAccent.Render("(bound)")
		}
		out = append(out, fmt.Sprintf("  %d. %-11s %s %s", i+1, src, mark, note))
	}
	return append(out, "")
}

// sourceStatus answers what one source has to say about an application.
func sourceStatus(app config.Application, src config.Source, snap *inventory.Snapshot) (mark, note string) {
	id, configured := app.Package.ID(src)

	switch {
	case !configured || id == "":
		return stRed.Render("✗"), stDim.Render("no id declared")
	case src.Normalize() != config.SourceGitHub && !snap.Available(src):
		return stYellow.Render("·"), stDim.Render("manager not installed")
	}

	if e, found := snap.Lookup(src, id); found {
		return stGreen.Render("✓"), "installed " + e.Version
	}
	return stDim.Render("·"), stDim.Render("available")
}

// renderDetail describes the row under the cursor.
//
// The interesting part is not the package metadata — it is why the row is in
// the state it is in: which source owns it, whether that was the preferred one,
// and what the other sources answered. That is recorded in the state file and
// has so far been invisible.
func renderDetail(r row, st *state.State, snap *inventory.Snapshot, width int) []string {
	if r.name() == "" {
		return []string{stDim.Render("no application selected")}
	}
	// An upgradable row carries both: the machine entry it came from and the
	// application that declares it. The declaration is the more useful view.
	if r.entrySrc != "" && r.app.Name == "" {
		return renderInstalledDetail(r, width)
	}

	var out []string
	field := func(label, value string) {
		if value == "" {
			return
		}
		out = append(out, stDim.Render(fmt.Sprintf("%-14s", label))+value)
	}

	app := r.app
	field("Application", stBold.Render(app.Name))
	field("Category", r.category)
	field("Action", string(app.EffectiveAction()))
	field("Status", r.state.label())
	out = append(out, "")

	// Ownership.
	switch r.res.Kind {
	case runner.ResolutionBound:
		field("Managed by", string(r.res.Source)+"  "+stDim.Render("(bound)"))
	case runner.ResolutionDetected:
		field("Managed by", string(r.res.Source)+"  "+stDim.Render("(detected)"))
	case runner.ResolutionConflict:
		field("Managed by", stRed.Render("conflict — press a to pick one"))
	case runner.ResolutionNoSource:
		field("Managed by", stYellow.Render("no source declared"))
	default:
		field("Would use", string(r.res.Source))
	}

	if e, ok := st.Get(app.Name); ok {
		if !e.InstalledAt.IsZero() {
			field("Bound since", e.InstalledAt.Local().Format("2006-01-02 15:04"))
		}
	}

	version := r.version
	if version == "" {
		version = stDim.Render("—")
	}
	if app.Version != "" && !strings.EqualFold(app.Version, "latest") {
		version += "   " + stDim.Render("declared "+app.Version)
	}
	field("Version", version)

	// Matching the declaration and being current are different statements.
	// A pinned application can sit exactly where the file asks and still be
	// several releases behind, and nothing else on this screen would say so.
	if r.available != "" {
		if r.frozen {
			field("Update", stYellow.Render(r.available)+"  "+
				stDim.Render("held back by the declared version"))
		} else {
			field("Update", stYellow.Render(r.available)+"  "+
				stDim.Render("press i to upgrade"))
		}
	}
	out = append(out, "")

	// The preference chain, with what each source currently answers.
	if lines := renderSourceChain(r, snap); len(lines) > 0 {
		out = append(out, lines...)
	}

	// Identifiers, so the user can copy them straight into the config.
	if configured := app.Package.Configured(); len(configured) > 0 {
		out = append(out, stDim.Render("Package ids"))
		for _, src := range configured {
			id, _ := app.Package.ID(src)
			out = append(out, fmt.Sprintf("  %-11s %s", src, id))
		}
		out = append(out, "")
	}

	if len(app.Tags) > 0 {
		field("Tags", strings.Join(app.Tags, ", "))
	}
	if app.Pre(config.ActionInstall) != "" {
		field("pre_install", app.Pre(config.ActionInstall))
	}
	if app.Post(config.ActionInstall) != "" {
		field("post_install", app.Post(config.ActionInstall))
	}
	if app.InfoURL != "" {
		field("Homepage", stCyan.Render(app.InfoURL))
	}

	if app.Description != "" {
		out = append(out, "")
		out = append(out, stDim.Render("Description"))
		out = append(out, draw.Wrap(app.Description, max(width-2, 8))...)
	}

	return out
}

func renderInstalledDetail(r row, width int) []string {
	var out []string
	field := func(label, value string) {
		out = append(out, stDim.Render(fmt.Sprintf("%-14s", label))+value)
	}

	field("Package", stBold.Render(r.entry.ID))
	field("Source", string(r.entrySrc))
	if r.entry.Version != "" {
		field("Version", r.entry.Version)
	}
	out = append(out, "")

	// The label asks a yes/no question, so the value answers one. The declared
	// name is a separate line, and only when it differs from the package id —
	// otherwise it just repeats what is two lines above.
	if r.configName != "" {
		field("In config", stGreen.Render("yes"))
		if !strings.EqualFold(r.configName, r.entry.ID) {
			field("Declared as", r.configName)
		}
		out = append(out, "")
		out = append(out, draw.Wrap("This package is already declared, so apptide install will keep it.", max(width-2, 8))...)
		return out
	}

	field("In config", stYellow.Render("no"))
	out = append(out, "")
	out = append(out, draw.Wrap("Not declared in the config: apptide install will neither install nor remove it. Press y to copy this entry, then paste it in apptide edit.", max(width-2, 8))...)
	out = append(out, "")
	for _, line := range strings.Split(strings.TrimRight(yamlSnippet([]row{r}), "\n"), "\n") {
		out = append(out, stDim.Render(line))
	}
	return out
}

// yamlSnippet renders installed packages as v2 application entries, ready to
// paste into a config.
//
// The browser deliberately does not write the user's YAML — `apptide edit`
// owns that file, with its own undo history and validation. Handing over the
// text keeps a single writer while still saving the retyping.
func yamlSnippet(rows []row) string {
	var b strings.Builder
	for _, r := range rows {
		if r.entrySrc == "" {
			continue
		}
		fmt.Fprintf(&b, "  - name: %s\n", r.entry.ID)
		fmt.Fprintf(&b, "    source: %s\n", r.entrySrc)
		// `latest` rather than the installed version: adopting a package should
		// not silently pin it to whatever happens to be on the machine today.
		fmt.Fprintf(&b, "    version: latest\n")
		fmt.Fprintf(&b, "    package:\n")
		fmt.Fprintf(&b, "      %s:\n", r.entrySrc)
		fmt.Fprintf(&b, "        id: %s\n", r.entry.ID)
	}
	return b.String()
}
