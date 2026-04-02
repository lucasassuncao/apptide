package runner

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lucasassuncao/apptide/internal/installer"
)

// ExportOptions configures the export command.
type ExportOptions struct {
	Output      string // file path; empty = stdout
	GitHubToken string
}

// Export generates a packages.yaml from all installed packages detected by
// the available package managers (winget, scoop, chocolatey).
func Export(opts ExportOptions) error {
	out := io.Writer(os.Stdout)
	if opts.Output != "" {
		f, err := os.Create(opts.Output)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer f.Close()
		out = f
	}

	fmt.Fprintf(out, "# Exported by apptide on %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(out, "# Run: apptide install --config <this-file>\n\n")

	any := false

	if pkgs, err := exportWinget(); err != nil {
		fmt.Fprintf(os.Stderr, "%s⚠ winget export skipped: %v%s\n", yellow, err, reset)
	} else if len(pkgs) > 0 {
		any = true
		writeCategory(out, "Winget", pkgs)
	}

	if pkgs, err := exportScoop(); err != nil {
		fmt.Fprintf(os.Stderr, "%s⚠ scoop export skipped: %v%s\n", yellow, err, reset)
	} else if len(pkgs) > 0 {
		any = true
		writeCategory(out, "Scoop", pkgs)
	}

	if pkgs, err := exportChocolatey(); err != nil {
		fmt.Fprintf(os.Stderr, "%s⚠ chocolatey export skipped: %v%s\n", yellow, err, reset)
	} else if len(pkgs) > 0 {
		any = true
		writeCategory(out, "Chocolatey", pkgs)
	}

	if !any {
		fmt.Fprintln(os.Stderr, "no package manager found or no packages detected")
	}

	if opts.Output != "" {
		fmt.Printf("%s✓%s written to %s\n", green, reset, opts.Output)
	}
	return nil
}

// exportEntry is a minimal representation used while building the YAML output.
type exportEntry struct {
	name    string
	source  string
	id      string
	version string
}

// writeCategory writes a YAML category block to w.
func writeCategory(w io.Writer, category string, pkgs []exportEntry) {
	fmt.Fprintf(w, "%s:\n", category)
	for _, p := range pkgs {
		fmt.Fprintf(w, "  - name: %q\n", p.name)
		fmt.Fprintf(w, "    source: %s\n", p.source)
		fmt.Fprintf(w, "    id: %q\n", p.id)
		if p.version != "" && p.version != "Unknown" {
			fmt.Fprintf(w, "    version: %q\n", p.version)
		}
		fmt.Fprintf(w, "    action: install\n\n")
	}
}

// ── winget ───────────────────────────────────────────────────────────────────

type wingetExportFile struct {
	Sources []struct {
		Packages []struct {
			PackageIdentifier string `json:"PackageIdentifier"`
			Version           string `json:"Version"`
		} `json:"Packages"`
	} `json:"Sources"`
}

func exportWinget() ([]exportEntry, error) {
	if _, err := exec.LookPath("winget"); err != nil {
		return nil, fmt.Errorf("winget not in PATH")
	}

	tmp, err := os.CreateTemp("", "apptide-winget-*.json")
	if err != nil {
		return nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	out, err := exec.Command("winget", "export", "--output", tmp.Name(), "--accept-source-agreements").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("winget export: %s", strings.TrimSpace(string(out)))
	}

	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return nil, err
	}

	var export wingetExportFile
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("parsing winget export JSON: %w", err)
	}

	var result []exportEntry
	for _, src := range export.Sources {
		for _, pkg := range src.Packages {
			result = append(result, exportEntry{
				name:    pkg.PackageIdentifier,
				source:  installer.SourceWinget,
				id:      pkg.PackageIdentifier,
				version: pkg.Version,
			})
		}
	}
	return result, nil
}

// ── scoop ────────────────────────────────────────────────────────────────────

type scoopExportFile struct {
	Apps []struct {
		Name    string `json:"Name"`
		Version string `json:"Version"`
	} `json:"apps"`
}

func exportScoop() ([]exportEntry, error) {
	if _, err := exec.LookPath("scoop"); err != nil {
		return nil, fmt.Errorf("scoop not in PATH")
	}

	// scoop export outputs JSON to stdout
	out, err := exec.Command("scoop", "export").Output()
	if err != nil {
		return nil, fmt.Errorf("scoop export: %w", err)
	}

	var export scoopExportFile
	if err := json.Unmarshal(out, &export); err != nil {
		return nil, fmt.Errorf("parsing scoop export JSON: %w", err)
	}

	var result []exportEntry
	for _, app := range export.Apps {
		result = append(result, exportEntry{
			name:    app.Name,
			source:  installer.SourceScoop,
			id:      app.Name,
			version: app.Version,
		})
	}
	return result, nil
}

// ── chocolatey ───────────────────────────────────────────────────────────────

func exportChocolatey() ([]exportEntry, error) {
	if _, err := exec.LookPath("choco"); err != nil {
		return nil, fmt.Errorf("choco not in PATH")
	}

	// --limit-output: pipe-separated "id|version" lines, no headers
	out, err := exec.Command("choco", "list", "--local-only", "--limit-output").Output()
	if err != nil {
		return nil, fmt.Errorf("choco list: %w", err)
	}

	var result []exportEntry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		id := parts[0]
		version := ""
		if len(parts) == 2 {
			version = parts[1]
		}
		result = append(result, exportEntry{
			name:    id,
			source:  installer.SourceChocolatey,
			id:      id,
			version: version,
		})
	}
	return result, nil
}
