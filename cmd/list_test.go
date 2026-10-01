package cmd

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lucasassuncao/apptide/internal/output"
	"github.com/lucasassuncao/bezel/draw"
)

func runList(t *testing.T, categoriesOnly bool) string {
	t.Helper()
	prev := listCategories
	listCategories = categoriesOnly
	t.Cleanup(func() { listCategories = prev })

	return captureCmdStdout(t, func() {
		if err := listCmd.RunE(listCmd, nil); err != nil {
			t.Fatalf("list: %v", err)
		}
	})
}

func TestListJSONSortsByNameWithinCategory(t *testing.T) {
	useConfig(t, sampleConfig)
	output.Set("json")
	t.Cleanup(func() { output.Set("table") })

	var apps []jsonApp
	if err := json.Unmarshal([]byte(runList(t, false)), &apps); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	var got []string
	for _, a := range apps {
		got = append(got, a.Category+"/"+a.Name)
	}
	want := []string{"Development/Git", "Development/Neovim", "Utilities/7-Zip"}
	if !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}

	// Sources keep the preference order, and the action is the effective one.
	if !slices.Equal(apps[1].Sources, []string{"winget", "scoop"}) {
		t.Errorf("Neovim sources = %v, want [winget scoop]", apps[1].Sources)
	}
	if apps[0].Action != "install" || apps[2].Action != "skip" {
		t.Errorf("actions = %q, %q, want install, skip", apps[0].Action, apps[2].Action)
	}
}

func TestListTableGroupsByCategory(t *testing.T) {
	useConfig(t, sampleConfig)

	out := runList(t, false)
	dev, utils := strings.Index(out, "[Development]"), strings.Index(out, "[Utilities]")
	if dev < 0 || utils < 0 {
		t.Fatalf("category headers missing:\n%s", out)
	}
	if git := strings.Index(out, "Git"); git < dev || git > utils {
		t.Errorf("Git is not listed under Development:\n%s", out)
	}
}

// Columns are measured in screen cells: padding by bytes or runes pushed
// the source of a wide-character name right of the others.
func TestListTableAlignsWideNames(t *testing.T) {
	useConfig(t, `schema_version: 2
applications:
  - name: "日本語-tools"
    source: scoop
    package:
      scoop:
        id: jp
  - name: "git"
    source: winget
    package:
      winget:
        id: Git.Git
`)

	cols := map[string]int{}
	for _, line := range strings.Split(ansi.Strip(runList(t, false)), "\n") {
		for _, src := range []string{"scoop", "winget"} {
			if i := strings.Index(line, src); i >= 0 {
				cols[src] = draw.Width(line[:i])
			}
		}
	}
	if len(cols) != 2 || cols["scoop"] != cols["winget"] {
		t.Errorf("source columns start at %v, want the same cell", cols)
	}
}

func TestListCategoriesCountsApplications(t *testing.T) {
	useConfig(t, sampleConfig)

	out := runList(t, true)
	if !strings.Contains(out, "Development") || !strings.Contains(out, "2 application(s)") {
		t.Errorf("Development count missing:\n%s", out)
	}
	if strings.Contains(out, "Neovim") {
		t.Errorf("--categories listed applications:\n%s", out)
	}
}
