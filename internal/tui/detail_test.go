package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"gopkg.in/yaml.v3"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/inventory"
	"github.com/lucasassuncao/apptide/internal/state"
)

// "ação" is 4 cells but 6 bytes: measured in bytes, "ação ação" (9 cells)
// did not fit in 12 and broke early.
func TestDetailDescriptionWrapsByScreenCells(t *testing.T) {
	r := row{app: config.Application{Name: "Tool", Description: "ação ação ação"}}
	out := renderDetail(r, state.New(t.TempDir()+"/state.json"), nil, 14)

	desc := -1
	for i, l := range out {
		if ansi.Strip(l) == "Description" {
			desc = i
		}
	}
	if desc < 0 {
		t.Fatalf("no Description block:\n%v", out)
	}
	rows := out[desc+1:]
	if len(rows) == 0 || ansi.Strip(rows[0]) != "ação ação" {
		t.Fatalf("first description row = %q, want %q", rows, "ação ação")
	}
	for _, l := range rows {
		if w := ansi.StringWidth(l); w > 12 {
			t.Errorf("row %q is %d cells, past the 12 the pane allows", l, w)
		}
	}
}

func TestYamlSnippetIsAValidApplicationEntry(t *testing.T) {
	rows := []row{
		{entry: inventory.Entry{ID: "choco-cleaner", Version: "0.0.9"}, entrySrc: config.SourceChocolatey},
		{entry: inventory.Entry{ID: "dotnetfx"}, entrySrc: config.SourceChocolatey},
	}

	snippet := yamlSnippet(rows)

	// The snippet must parse as the applications list of a v2 config.
	var doc struct {
		Applications []config.Application `yaml:"applications"`
	}
	if err := yaml.Unmarshal([]byte("applications:\n"+snippet), &doc); err != nil {
		t.Fatalf("snippet does not parse: %v\n%s", err, snippet)
	}
	if len(doc.Applications) != 2 {
		t.Fatalf("parsed %d applications, want 2:\n%s", len(doc.Applications), snippet)
	}

	first := doc.Applications[0]
	if first.Name != "choco-cleaner" {
		t.Errorf("name = %q", first.Name)
	}
	// Adopting must not pin the package to whatever is installed today.
	if first.Version != "latest" {
		t.Errorf("version = %q, want latest", first.Version)
	}
	if strings.Contains(snippet, "# installed") {
		t.Errorf("snippet still carries the installed-version comment:\n%s", snippet)
	}
	if !first.Source.Contains(config.SourceChocolatey) {
		t.Errorf("source = %v", first.Source)
	}
	if id, ok := first.Package.ID(config.SourceChocolatey); !ok || id != "choco-cleaner" {
		t.Errorf("package id = %q (configured: %v)", id, ok)
	}
}

// "In config" asks a yes/no question, so both branches answer one. Answering
// with a name in one case and "no" in the other made the same label mean two
// different kinds of thing.
func TestInstalledDetailAnswersInConfigWithYesOrNo(t *testing.T) {
	tracked := row{
		entry:      inventory.Entry{ID: "Google.Chrome", Version: "150.0"},
		entrySrc:   config.SourceWinget,
		configName: "Chrome",
	}
	untracked := row{
		entry:    inventory.Entry{ID: "Piriform.CCleaner"},
		entrySrc: config.SourceWinget,
	}

	yes := strings.Join(renderInstalledDetail(tracked, 60), "\n")
	if !strings.Contains(ansi.Strip(yes), "In config     yes") {
		t.Errorf("tracked package does not answer yes:\n%s", ansi.Strip(yes))
	}
	if !strings.Contains(ansi.Strip(yes), "Declared as   Chrome") {
		t.Errorf("the declared name is missing when it differs from the id:\n%s", ansi.Strip(yes))
	}

	no := strings.Join(renderInstalledDetail(untracked, 60), "\n")
	if !strings.Contains(ansi.Strip(no), "In config     no") {
		t.Errorf("untracked package does not answer no:\n%s", ansi.Strip(no))
	}

	// When the declared name is just the package id, repeating it is noise.
	same := tracked
	same.configName = "Google.Chrome"
	if out := ansi.Strip(strings.Join(renderInstalledDetail(same, 60), "\n")); strings.Contains(out, "Declared as") {
		t.Errorf("declared name repeated the package id:\n%s", out)
	}
}
