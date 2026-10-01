package config

import (
	"os"
	"strings"
	"testing"
)

func sampleApp(name, id string) Application {
	return Application{
		Name:    name,
		Source:  Sources{SourceWinget},
		Package: Packages{Winget: &WingetSpec{ID: id}},
	}
}

func TestAppendApplicationsKeepsCommentsAndOrder(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "packages.yaml", `# apptide config
# every one of these comments must survive

schema_version: 2

import:
  - ./conf/extra.yaml

applications:
  # the first one
  - name: "Git"
    source: winget
    package:
      winget:
        id: Git.Git
`)

	if err := AppendApplications(path, []Application{sampleApp("Chrome", "Google.Chrome")}); err != nil {
		t.Fatalf("AppendApplications: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)

	for _, want := range []string{
		"# apptide config",
		"# every one of these comments must survive",
		"# the first one",
		"import:",
		"  - ./conf/extra.yaml",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("append dropped %q:\n%s", want, text)
		}
	}

	cfg, err := LoadWithImports(path)
	if err == nil && len(cfg.Applications) != 2 {
		t.Errorf("config has %d applications, want 2:\n%s", len(cfg.Applications), text)
	}
	if !strings.Contains(text, "Google.Chrome") {
		t.Errorf("new entry missing:\n%s", text)
	}
	if strings.Index(text, "Git.Git") > strings.Index(text, "Google.Chrome") {
		t.Errorf("new entry was not appended after the existing one:\n%s", text)
	}
}

// The entries have to land inside the applications block even when another
// top-level key follows it, or the file stops parsing.
func TestAppendApplicationsInsertsBeforeTheNextTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "packages.yaml", `applications:
  - name: "Git"
    source: winget
    package:
      winget:
        id: Git.Git

defaults:
  category: Tools
`)

	if err := AppendApplications(path, []Application{sampleApp("Chrome", "Google.Chrome")}); err != nil {
		t.Fatalf("AppendApplications: %v", err)
	}

	cfg, err := LoadWithImports(path)
	if err != nil {
		out, _ := os.ReadFile(path)
		t.Fatalf("config no longer parses: %v\n%s", err, out)
	}
	if len(cfg.Applications) != 2 {
		t.Fatalf("got %d applications, want 2", len(cfg.Applications))
	}
	for _, app := range cfg.Applications {
		if app.Category != "Tools" {
			t.Errorf("%s has category %q — the defaults block was not preserved", app.Name, app.Category)
		}
	}
}

func TestAppendApplicationsCreatesTheBlockWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "packages.yaml", "schema_version: 2\n")

	if err := AppendApplications(path, []Application{sampleApp("Chrome", "Google.Chrome")}); err != nil {
		t.Fatalf("AppendApplications: %v", err)
	}

	cfg, err := LoadWithImports(path)
	if err != nil {
		out, _ := os.ReadFile(path)
		t.Fatalf("config does not parse: %v\n%s", err, out)
	}
	if len(cfg.Applications) != 1 || cfg.Applications[0].Name != "Chrome" {
		t.Errorf("applications = %+v", cfg.Applications)
	}
}

func TestAppendApplicationsHandlesEmptyList(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "packages.yaml", "applications: []\n")

	if err := AppendApplications(path, []Application{sampleApp("Chrome", "Google.Chrome")}); err != nil {
		t.Fatalf("AppendApplications: %v", err)
	}

	cfg, err := LoadWithImports(path)
	if err != nil {
		out, _ := os.ReadFile(path)
		t.Fatalf("config does not parse: %v\n%s", err, out)
	}
	if len(cfg.Applications) != 1 {
		t.Errorf("got %d applications, want 1", len(cfg.Applications))
	}
}

func TestAppendApplicationsAppendsSeveralAtOnce(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "packages.yaml", "applications:\n")

	apps := []Application{
		sampleApp("Chrome", "Google.Chrome"),
		sampleApp("Firefox", "Mozilla.Firefox"),
		sampleApp("VLC", "VideoLAN.VLC"),
	}
	if err := AppendApplications(path, apps); err != nil {
		t.Fatalf("AppendApplications: %v", err)
	}

	cfg, err := LoadWithImports(path)
	if err != nil {
		out, _ := os.ReadFile(path)
		t.Fatalf("config does not parse: %v\n%s", err, out)
	}
	if len(cfg.Applications) != 3 {
		t.Fatalf("got %d applications, want 3", len(cfg.Applications))
	}
}

func TestAppendApplicationsLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "packages.yaml", "applications:\n")

	if err := AppendApplications(path, []Application{sampleApp("Chrome", "Google.Chrome")}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "packages.yaml" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory holds %v, want only packages.yaml", names)
	}
}
