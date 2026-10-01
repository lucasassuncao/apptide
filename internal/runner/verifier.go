package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/lucasassuncao/apptide/internal/inventory"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/output"
	"github.com/lucasassuncao/apptide/internal/state"
)

// VerifyOptions configures a verification run.
type VerifyOptions struct {
	ConfigPath  string
	Category    string
	Source      string
	Tags        []string
	InstallDir  string
	GitHubToken string
	StatePath   string
}

// verifyResult is one resolved application, shared by the table and JSON paths.
type verifyResult struct {
	app      config.Application
	category string
	source   config.Source
	status   string // installed | not_found | skip | conflict | no_source | unavailable | unknown_source
	version  string
	detail   string
}

// Verify checks which applications from the config are installed without
// making any changes.
func Verify(opts VerifyOptions) error {
	cfg, err := config.LoadWithImports(opts.ConfigPath)
	if err != nil {
		return err
	}

	instOpts := installer.Options{
		GitHubToken:       opts.GitHubToken,
		DefaultInstallDir: opts.InstallDir,
	}

	apps, err := selectApps(cfg, opts.Category, opts.Source, opts.Tags)
	if err != nil {
		return err
	}
	if len(apps) == 0 {
		return nil
	}

	statePath := opts.StatePath
	if statePath == "" {
		statePath = state.DefaultPath()
	}
	// Verify never writes, so a damaged state file must not stop it: fall back
	// to detection with an empty state.
	st, _, err := state.LoadOrReset(statePath)
	if err != nil {
		st = state.New(statePath)
	}

	// Verify is the command that reads the whole config and touches nothing, so
	// it is the one that gained the most from asking each manager once.
	ctx := context.Background()
	probe := NewInventoryProber(inventory.Installed(ctx), instOpts)

	results := make([]verifyResult, 0, len(apps))
	for _, app := range apps {
		results = append(results, verifyOne(app, st, instOpts, probe))
	}

	if output.IsJSON() {
		return verifyJSON(results)
	}
	return verifyTable(results)
}

func verifyOne(app config.Application, st *state.State, instOpts installer.Options, probe Prober) verifyResult {
	out := verifyResult{app: app, category: app.CategoryOrDefault()}

	if app.EffectiveAction() == config.ActionSkip {
		out.status = "skip"
		return out
	}

	res := ResolveSource(app, st, probe)
	out.source = res.Source

	switch res.Kind {
	case ResolutionNoSource:
		out.status = "no_source"
		return out
	case ResolutionConflict:
		names := make([]string, len(res.Conflicts))
		for i, s := range res.Conflicts {
			names[i] = string(s)
		}
		out.status = "conflict"
		out.detail = "installed via " + strings.Join(names, " and ")
		return out
	}

	inst, err := installer.Resolve(res.Source, instOpts)
	if err != nil {
		out.status = "unknown_source"
		return out
	}
	if !inst.IsAvailable() {
		out.status = "unavailable"
		out.detail = inst.Name() + " not in PATH"
		return out
	}

	// The snapshot already holds the answer for every inventoried source.
	installed, version := probe.Probe(res.Source, app)
	out.version = version
	if installed {
		out.status = "installed"
	} else {
		out.status = "not_found"
	}
	return out
}

func verifyTable(results []verifyResult) error {
	rows := make([]tableRow, len(results))
	for i, r := range results {
		rows[i] = tableRow{app: r.app, category: r.category}
	}
	calcWidths(rows)

	fmt.Println(tableHeader())

	var total, installed, missing, skipped, unavailable int

	for _, r := range results {
		total++
		method := displayMethod(r.source)

		var status string
		switch r.status {
		case "skip":
			skipped++
			status = lgGray.Render("skip")
		case "no_source":
			unavailable++
			status = lgYellow.Render("no source declared")
		case "conflict":
			unavailable++
			status = lgYellow.Render("conflict: " + r.detail)
		case "unknown_source":
			unavailable++
			status = lgYellow.Render("unknown source")
		case "unavailable":
			unavailable++
			status = lgYellow.Render(r.detail)
		case "installed":
			installed++
			status = lgGreen.Render("installed")
			if r.version != "" {
				status += "  " + lgGray.Render(r.version)
			}
		default:
			missing++
			status = lgRed.Render("not found")
		}
		fmt.Println(fmtRow(r.app.Name, r.category, method, status))
	}

	sep := rule(statusWidth)
	line := fmt.Sprintf("total %-4d  %s  %s  %s  %s",
		total,
		lgGreen.Render(fmt.Sprintf("installed %d", installed)),
		lgRed.Render(fmt.Sprintf("missing %d", missing)),
		lgGray.Render(fmt.Sprintf("skip %d", skipped)),
		lgYellow.Render(fmt.Sprintf("unavailable %d", unavailable)),
	)
	fmt.Println("\n" + sep + "\n" + line)

	if missing > 0 {
		return fmt.Errorf("%d application(s) not installed", missing)
	}
	return nil
}

// verifyJSONRow is the JSON shape emitted by `verify --output json`.
type verifyJSONRow struct {
	Name           string   `json:"name"`
	Category       string   `json:"category"`
	Sources        []string `json:"sources"`
	ResolvedSource string   `json:"resolved_source,omitempty"`
	Action         string   `json:"action"`
	Installed      bool     `json:"installed"`
	CurrentVersion string   `json:"current_version,omitempty"`
	Status         string   `json:"status"`
	Detail         string   `json:"detail,omitempty"`
}

func verifyJSON(results []verifyResult) error {
	rows := make([]verifyJSONRow, 0, len(results))
	var missing int

	for _, r := range results {
		sources := make([]string, len(r.app.Source))
		for i, s := range r.app.Source {
			sources[i] = string(s)
		}
		if r.status == "not_found" {
			missing++
		}
		rows = append(rows, verifyJSONRow{
			Name:           r.app.Name,
			Category:       r.category,
			Sources:        sources,
			ResolvedSource: string(r.source),
			Action:         string(r.app.EffectiveAction()),
			Installed:      r.status == "installed",
			CurrentVersion: r.version,
			Status:         r.status,
			Detail:         r.detail,
		})
	}

	output.PrintJSON(rows)

	if missing > 0 {
		return fmt.Errorf("%d application(s) not installed", missing)
	}
	return nil
}
