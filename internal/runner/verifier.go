package runner

import (
	"fmt"
	"os"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/output"
)

type verifyRow struct {
	pkg config.Package
	cat string
}

// VerifyOptions configures a verification run.
type VerifyOptions struct {
	ConfigPath  string
	Category    string
	Source      string
	InstallDir  string
	GitHubToken string
}

// Verify checks which packages from the config are installed without making any changes.
func Verify(opts VerifyOptions) error {
	cfg, err := config.LoadWithImports(opts.ConfigPath)
	if err != nil {
		return err
	}

	instOpts := installer.Options{
		GitHubToken:       opts.GitHubToken,
		DefaultInstallDir: opts.InstallDir,
	}

	categories := cfg.Categories()
	if opts.Category != "" {
		if _, ok := cfg[opts.Category]; !ok {
			fmt.Fprintf(os.Stderr, "%serror:%s category %q not found\n", red, reset, opts.Category)
			fmt.Fprintf(os.Stderr, "available: %s\n", strings.Join(categories, ", "))
			return fmt.Errorf("category %q not found", opts.Category)
		}
		categories = []string{opts.Category}
	}

	var rows []verifyRow
	for _, cat := range categories {
		pkgs := cfg[cat]
		if opts.Source != "" {
			pkgs = filterBySource(pkgs, opts.Source)
		}
		for _, pkg := range pkgs {
			rows = append(rows, verifyRow{pkg: pkg, cat: cat})
		}
	}
	if len(rows) == 0 {
		return nil
	}

	if output.IsJSON() {
		return verifyJSON(rows, instOpts)
	}

	// Set wApp based on actual package names before rendering.
	tuiRows := make([]tableRow, len(rows))
	for i, r := range rows {
		tuiRows[i] = tableRow{pkg: r.pkg, category: r.cat}
	}
	calcWidths(tuiRows)

	fmt.Println(renderVerifyHeader())

	var total, installed, missing, skipped, unavailable int

	for _, row := range rows {
		total++
		action := strings.ToLower(row.pkg.Action)
		if action == "" {
			action = "install"
		}

		method := displayMethod(row.pkg.Source)

		if action == "skip" {
			skipped++
			status := lgGray.Render("skip")
			fmt.Println(fmtRow(row.pkg.Name, row.cat, method, status))
			continue
		}

		inst, err := installer.Resolve(row.pkg.Source, instOpts)
		if err != nil {
			unavailable++
			status := lgYellow.Render("unknown source")
			fmt.Println(fmtRow(row.pkg.Name, row.cat, method, status))
			continue
		}

		if !inst.IsAvailable() {
			unavailable++
			status := lgYellow.Render(inst.Name() + " not in PATH")
			fmt.Println(fmtRow(row.pkg.Name, row.cat, method, status))
			continue
		}

		ok, version := inst.Check(row.pkg)
		if ok {
			installed++
			s := lgGreen.Render("installed")
			if version != "" {
				s += "  " + lgGray.Render(version)
			}
			fmt.Println(fmtRow(row.pkg.Name, row.cat, method, s))
		} else {
			missing++
			fmt.Println(fmtRow(row.pkg.Name, row.cat, method, lgRed.Render("not found")))
		}
	}

	sep := lgGray.Render(strings.Repeat("─", wApp+wCat+wMethod+wStatus+8))
	line := fmt.Sprintf("total %-4d  %s  %s  %s  %s",
		total,
		lgGreen.Render(fmt.Sprintf("installed %d", installed)),
		lgRed.Render(fmt.Sprintf("missing %d", missing)),
		lgGray.Render(fmt.Sprintf("skip %d", skipped)),
		lgYellow.Render(fmt.Sprintf("unavailable %d", unavailable)),
	)
	fmt.Println("\n" + sep + "\n" + line)

	if missing > 0 {
		return fmt.Errorf("%d package(s) not installed", missing)
	}
	return nil
}

// verifyRow is the internal type used during table collection; json struct is separate.
type verifyJSONRow struct {
	Name           string `json:"name"`
	Category       string `json:"category"`
	Source         string `json:"source"`
	Action         string `json:"action"`
	Installed      bool   `json:"installed"`
	CurrentVersion string `json:"current_version,omitempty"`
	Status         string `json:"status"`
}

func verifyJSON(rows []verifyRow, instOpts installer.Options) error {
	var result []verifyJSONRow
	var missing int

	for _, row := range rows {
		action := strings.ToLower(row.pkg.Action)
		if action == "" {
			action = "install"
		}

		entry := verifyJSONRow{
			Name:     row.pkg.Name,
			Category: row.cat,
			Source:   row.pkg.Source,
			Action:   action,
		}

		if action == "skip" {
			entry.Status = "skip"
			result = append(result, entry)
			continue
		}

		inst, err := installer.Resolve(row.pkg.Source, instOpts)
		if err != nil {
			entry.Status = "unknown_source"
			result = append(result, entry)
			continue
		}

		if !inst.IsAvailable() {
			entry.Status = inst.Name() + "_not_in_path"
			result = append(result, entry)
			continue
		}

		ok, version := inst.Check(row.pkg)
		entry.Installed = ok
		entry.CurrentVersion = version
		if ok {
			entry.Status = "installed"
		} else {
			entry.Status = "not_found"
			missing++
		}
		result = append(result, entry)
	}

	output.PrintJSON(result)

	if missing > 0 {
		return fmt.Errorf("%d package(s) not installed", missing)
	}
	return nil
}

func renderVerifyHeader() string {
	h := fmt.Sprintf("\n  %s  %s  %s  %s",
		lgBold.Render(pad("Application", wApp)),
		lgBold.Render(pad("Category", wCat)),
		lgBold.Render(pad("Method", wMethod)),
		lgBold.Render(pad("Status", wStatus)),
	)
	sep := lgGray.Render(strings.Repeat("─", wApp+wCat+wMethod+wStatus+8))
	return h + "\n" + sep
}
