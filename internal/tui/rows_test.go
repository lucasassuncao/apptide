package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/inventory"
)

// The fourth column answers a different question per tab, so it is worth
// pinning both: "does apptide manage this?" on the machine listing, and "what
// does the file ask for?" on the config listing.
func TestManagedColumnOnTheInstalledTab(t *testing.T) {
	m := newTestModel(t, nil)
	m.setTab(tabInstalled)

	tracked := row{entry: inventory.Entry{ID: "Google.Chrome"}, entrySrc: config.SourceWinget, configName: "Chrome"}
	loose := row{entry: inventory.Entry{ID: "7zip.7zip"}, entrySrc: config.SourceWinget}

	if got := ansi.Strip(columnSpecs(tabInstalled)[1].value(tracked)); got != "true" {
		t.Errorf("declared package shows %q, want true", got)
	}
	if got := ansi.Strip(columnSpecs(tabInstalled)[1].value(loose)); got != "false" {
		t.Errorf("undeclared package shows %q, want false", got)
	}
	if got := columnSpecs(tabInstalled)[1].title; got != "Managed by apptide" {
		t.Errorf("column heading = %q", got)
	}
}

// A pinned application can be exactly where the file asks and still be behind.
func TestUpgradeMarkerShowsEvenWhenPinnedAndMatching(t *testing.T) {
	pinned := row{
		app:       config.Application{Name: "Node.js", Version: "20.11.1"},
		version:   "20.11.1",
		frozen:    true,
		available: "22.0.0",
	}
	got := ansi.Strip(pinned.installedLabel(true))
	if !strings.Contains(got, "↑") {
		t.Errorf("installed column = %q, want an upgrade marker", got)
	}

	current := row{app: config.Application{Name: "Git"}, version: "2.47.1"}
	if got := ansi.Strip(current.installedLabel(true)); strings.Contains(got, "↑") {
		t.Errorf("an application with no upgrade available shows %q", got)
	}
}

// The config tabs carry both versions, declared then installed, because they
// are different facts and reading one without the other says nothing about
// whether the machine matches the file.
func TestConfigTabShowsDeclaredAndInstalledVersions(t *testing.T) {
	// Nothing in the file is not the same statement as `version: latest`, even
	// though they behave alike.
	absent := row{app: config.Application{Name: "Git"}, version: "2.47.1"}
	if got := ansi.Strip(absent.declaredLabel()); got != "none (latest)" {
		t.Errorf("an application with no version field declares %q", got)
	}

	latest := row{app: config.Application{Name: "Git", Version: "latest"}, version: "2.47.1"}
	if got := ansi.Strip(latest.declaredLabel()); got != "latest" {
		t.Errorf("an explicit `version: latest` declares %q", got)
	}
	if got := ansi.Strip(latest.installedLabel(true)); got != "2.47.1" {
		t.Errorf("installed column = %q", got)
	}

	// Pinned and current: both columns agree, and the frozen marker rides with
	// the declaration, since pinning is a property of the file.
	matching := row{app: config.Application{Name: "Node.js", Version: "20.11.1"}, version: "20.11.1", frozen: true}
	if got := ansi.Strip(matching.declaredLabel()); got != "20.11.1 ★" {
		t.Errorf("declared column = %q, want the version and the frozen marker", got)
	}

	// Pinned and behind: the next install will move the machine.
	drifted := row{app: config.Application{Name: "Node.js", Version: "20.11.1"}, version: "20.10.0"}
	if got := ansi.Strip(drifted.declaredLabel()); got != "20.11.1" {
		t.Errorf("declared column = %q", got)
	}
	if got := ansi.Strip(drifted.installedLabel(true)); got != "20.10.0" {
		t.Errorf("installed column = %q", got)
	}

	// Nothing installed yet.
	missing := row{app: config.Application{Name: "Lazygit"}}
	if got := ansi.Strip(missing.installedLabel(true)); got != "—" {
		t.Errorf("installed column for a missing application = %q", got)
	}
}
