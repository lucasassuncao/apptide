package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/inventory"
)

// ExportOptions configures the export command.
type ExportOptions struct {
	Output string // file path; empty = stdout
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

	snap := inventory.Installed(context.Background())

	fmt.Fprintf(out, "# Exported by apptide on %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(out, "# Run: apptide install --config <this-file>\n\n")
	fmt.Fprintf(out, "schema_version: %d\n\napplications:\n", config.SchemaVersion)

	any := false
	// Category names mirror the manager, since a raw export has no better
	// grouping to offer.
	categories := map[config.Source]string{
		config.SourceWinget:     "Winget",
		config.SourceScoop:      "Scoop",
		config.SourceChocolatey: "Chocolatey",
	}

	for _, src := range []config.Source{config.SourceWinget, config.SourceScoop, config.SourceChocolatey} {
		if err := snap.Err(src); err != nil {
			fmt.Fprintln(os.Stderr, lgYellow.Render(fmt.Sprintf("⚠ %s export skipped: %v", src, err)))
			continue
		}
		entries := snap.All(src)
		if len(entries) == 0 {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
		any = true
		writeCategory(out, categories[src], src, entries)
	}

	if !any {
		fmt.Fprintln(os.Stderr, "no package manager found or no packages detected")
	}

	if opts.Output != "" {
		fmt.Printf("%s written to %s\n", lgGreen.Render("✓"), opts.Output)
	}
	return nil
}

// writeCategory writes the applications of one manager in v2 layout.
// The exported version is the one currently installed, which pins the
// application; users who want to track latest should drop the field.
func writeCategory(w io.Writer, category string, src config.Source, entries []inventory.Entry) {
	for _, e := range entries {
		fmt.Fprintf(w, "  - name: %q\n", e.ID)
		fmt.Fprintf(w, "    category: %s\n", category)
		fmt.Fprintf(w, "    source: %s\n", src)
		if e.Version != "" && e.Version != "Unknown" {
			fmt.Fprintf(w, "    version: %q\n", e.Version)
		}
		fmt.Fprintf(w, "    package:\n")
		fmt.Fprintf(w, "      %s:\n", src)
		fmt.Fprintf(w, "        id: %q\n\n", e.ID)
	}
}
