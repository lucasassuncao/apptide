package runner

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/lucasassuncao/apptide/internal/config"
)

// Columns are measured by what the terminal draws: padding by runes gave a wide
// character one cell, and pushed that row's status right of the others'.
func TestRowsLineUpWhateverTheNamesAreMadeOf(t *testing.T) {
	rows := []tableRow{
		{app: config.Application{Name: "日本語-tools", Source: config.Sources{config.SourceWinget}}, category: "dev"},
		{app: config.Application{Name: "git", Source: config.Sources{config.SourceScoop}}, category: "dev"},
	}
	calcWidths(rows)

	statusAt := func(line string) int {
		before, _, found := strings.Cut(line, "STATUS")
		if !found {
			t.Fatalf("no status in %q", line)
		}
		return ansi.StringWidth(before)
	}
	wide := fmtRow("日本語-tools", "dev", "winget", "STATUS")
	plain := fmtRow("git", "dev", "scoop", "STATUS")
	if statusAt(wide) != statusAt(plain) {
		t.Fatalf("the status column moved:\n%q\n%q", wide, plain)
	}
	if !strings.Contains(ansi.Strip(tableHeader()), "Application") {
		t.Error("the header lost its titles")
	}
}

// A dry run must not claim work it did not do. The JSON output already said
// "would_install" while the table said "installed".
func TestStatusLabelDoesNotClaimWorkInADryRun(t *testing.T) {
	install := tableRow{
		app:    config.Application{Name: "App", Action: config.ActionInstall},
		status: statusOK,
		dryRun: true,
	}
	if got := statusLabel(install); !strings.Contains(got, "would install") {
		t.Errorf("dry-run install label = %q, want \"would install\"", got)
	}

	remove := tableRow{
		app:    config.Application{Name: "App", Action: config.ActionUninstall},
		status: statusOK,
		dryRun: true,
	}
	if got := statusLabel(remove); !strings.Contains(got, "would uninstall") {
		t.Errorf("dry-run uninstall label = %q, want \"would uninstall\"", got)
	}

	// A real run still reports what happened.
	real := tableRow{
		app:    config.Application{Name: "App", Action: config.ActionInstall},
		status: statusOK,
	}
	if got := statusLabel(real); !strings.Contains(got, "installed") || strings.Contains(got, "would") {
		t.Errorf("real install label = %q, want \"installed\"", got)
	}
}
