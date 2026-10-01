package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadV2(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "packages.yaml", `
schema_version: 2

applications:
  - name: "VS Code"
    category: Development
    source: winget
    package:
      winget:
        id: "Microsoft.VisualStudioCode"
        scope: machine

  - name: "Lazygit"
    category: Development
    source: [winget, scoop, github]
    skip_upgrade: true
    hooks:
      post_install: "lazygit --version"
    package:
      winget: { id: "JesseDuffield.lazygit" }
      scoop: { id: "lazygit" }
      github: { id: "jesseduffield/lazygit" }
`)

	cfg, err := LoadWithImports(path)
	if err != nil {
		t.Fatalf("LoadWithImports: %v", err)
	}
	if cfg.SchemaVersion != 2 {
		t.Errorf("SchemaVersion = %d, want 2", cfg.SchemaVersion)
	}
	if len(cfg.Applications) != 2 {
		t.Fatalf("got %d applications, want 2", len(cfg.Applications))
	}

	code := cfg.Applications[0]
	if len(code.Source) != 1 || code.Source[0] != SourceWinget {
		t.Errorf("scalar source decoded as %v, want [winget]", code.Source)
	}
	if code.Package.Winget == nil || code.Package.Winget.Scope != "machine" {
		t.Errorf("winget block not decoded: %+v", code.Package.Winget)
	}
	if code.EffectiveAction() != ActionInstall {
		t.Errorf("EffectiveAction() = %q, want install", code.EffectiveAction())
	}

	lazy := cfg.Applications[1]
	if len(lazy.Source) != 3 || lazy.Source[2] != SourceGitHub {
		t.Errorf("list source decoded as %v", lazy.Source)
	}
	if !lazy.SkipUpgrade {
		t.Error("skip_upgrade not decoded")
	}
	if lazy.Post(ActionInstall) != "lazygit --version" {
		t.Errorf("Post() = %q", lazy.Post(ActionInstall))
	}
	if got := lazy.Package.Configured(); len(got) != 3 {
		t.Errorf("Configured() = %v, want 3 sources", got)
	}
}

func TestDefaultsAreAppliedAndFileScoped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "child.yaml", `
schema_version: 2
applications:
  - name: "ripgrep"
    source: scoop
    package:
      scoop: { id: "ripgrep" }
`)
	root := writeFile(t, dir, "root.yaml", `
schema_version: 2
import:
  - ./child.yaml
defaults:
  category: CLITools
  action: skip
  tags: [core]
applications:
  - name: "jq"
    source: winget
    package:
      winget: { id: "jqlang.jq" }
`)

	cfg, err := LoadWithImports(root)
	if err != nil {
		t.Fatalf("LoadWithImports: %v", err)
	}
	if len(cfg.Applications) != 2 {
		t.Fatalf("got %d applications, want 2", len(cfg.Applications))
	}

	jq := cfg.Applications[0]
	if jq.Category != "CLITools" || jq.EffectiveAction() != ActionSkip {
		t.Errorf("defaults not applied to jq: category=%q action=%q", jq.Category, jq.Action)
	}
	if len(jq.Tags) != 1 || jq.Tags[0] != "core" {
		t.Errorf("default tags not applied: %v", jq.Tags)
	}

	// The imported file must not inherit the importer's defaults.
	rg := cfg.Applications[1]
	if rg.Category != "" {
		t.Errorf("imported app inherited category %q from the importer", rg.Category)
	}
	if rg.EffectiveAction() != ActionInstall {
		t.Errorf("imported app inherited action %q", rg.EffectiveAction())
	}
	if cfg.Defaults != nil {
		t.Error("Defaults should be cleared after resolution")
	}
}

func TestMixedLayoutIsRejected(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "mixed.yaml", `
applications:
  - name: "jq"
    source: winget
    package:
      winget: { id: "jqlang.jq" }
Development:
  - name: "Git"
    source: winget
`)

	if _, err := LoadWithImports(path); err == nil {
		t.Fatal("a file mixing v1 categories with applications: was accepted")
	}
}

func TestUnknownTopLevelKeyIsRejected(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "bad.yaml", "packages: {}\n")

	if _, err := LoadWithImports(path); err == nil {
		t.Fatal("unknown scalar top-level key was accepted")
	}
}

func TestCircularImportIsDetected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", "import:\n  - ./b.yaml\n")
	writeFile(t, dir, "b.yaml", "import:\n  - ./a.yaml\n")

	if _, err := LoadWithImports(filepath.Join(dir, "a.yaml")); err == nil {
		t.Fatal("circular import was not detected")
	}
}

// `source: winget, scoop` is the obvious way to write a preference list, and
// YAML reads it as one string. Accepting it keeps the mistake from surfacing
// only at validation.
func TestSourcesAcceptsCommaSeparatedScalar(t *testing.T) {
	cases := map[string][]Source{
		"source: winget\n":                 {SourceWinget},
		"source: winget, scoop\n":          {SourceWinget, SourceScoop},
		"source: winget,scoop,github\n":    {SourceWinget, SourceScoop, SourceGitHub},
		"source: \"choco, winget\"\n":      {SourceChocolatey, SourceWinget},
		"source: [winget, scoop]\n":        {SourceWinget, SourceScoop},
		"source:\n  - winget\n  - scoop\n": {SourceWinget, SourceScoop},
	}

	for in, want := range cases {
		var doc struct {
			Source Sources `yaml:"source"`
		}
		if err := yaml.Unmarshal([]byte(in), &doc); err != nil {
			t.Fatalf("unmarshal %q: %v", in, err)
		}
		if len(doc.Source) != len(want) {
			t.Errorf("%q decoded to %v, want %v", in, doc.Source, want)
			continue
		}
		for i := range want {
			if doc.Source[i] != want[i] {
				t.Errorf("%q decoded to %v, want %v", in, doc.Source, want)
				break
			}
		}
	}
}

// Install and uninstall have separate hooks: a command named for installing
// must not run when the application is being removed.
func TestHooksAreActionSpecific(t *testing.T) {
	app := Application{Hooks: &Hooks{
		PreInstall:    "before install",
		PostInstall:   "after install",
		PreUninstall:  "before remove",
		PostUninstall: "after remove",
	}}

	if got := app.Pre(ActionInstall); got != "before install" {
		t.Errorf("Pre(install) = %q", got)
	}
	if got := app.Post(ActionInstall); got != "after install" {
		t.Errorf("Post(install) = %q", got)
	}
	if got := app.Pre(ActionUninstall); got != "before remove" {
		t.Errorf("Pre(uninstall) = %q", got)
	}
	if got := app.Post(ActionUninstall); got != "after remove" {
		t.Errorf("Post(uninstall) = %q", got)
	}

	// An application declaring only install hooks runs nothing on removal.
	installOnly := Application{Hooks: &Hooks{PostInstall: "code --install-extension go"}}
	if got := installOnly.Post(ActionUninstall); got != "" {
		t.Errorf("an install hook fired on removal: %q", got)
	}

	var none Application
	if none.Pre(ActionInstall) != "" || none.Post(ActionUninstall) != "" {
		t.Error("an application without hooks returned one")
	}
}

func TestSettingsAreReadAndExpanded(t *testing.T) {
	t.Setenv("APPTIDE_TEST_DIR", `C:\Tools`)
	t.Setenv("APPTIDE_TEST_TOKEN", "ghp_secret")

	dir := t.TempDir()
	path := writeFile(t, dir, "packages.yaml", `
schema_version: 2
settings:
  install_dir: "${APPTIDE_TEST_DIR}/bin"
  add_to_path: true
  github_token: "${APPTIDE_TEST_TOKEN}"
applications:
  - name: "jq"
    source: winget
    package:
      winget: { id: "jqlang.jq" }
`)

	cfg, err := LoadWithImports(path)
	if err != nil {
		t.Fatalf("LoadWithImports: %v", err)
	}
	if cfg.Settings == nil {
		t.Fatal("settings block not decoded")
	}
	if got := cfg.Settings.InstallDir; got != `C:\Tools/bin` {
		t.Errorf("install_dir = %q, want the expanded path", got)
	}
	if got := cfg.Settings.GitHubToken; got != "ghp_secret" {
		t.Errorf("github_token = %q, want the expanded value", got)
	}
	if !cfg.Settings.AddToPath {
		t.Error("add_to_path not decoded")
	}
}

func TestOptionalAndRequiresAdminDecode(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "packages.yaml", `
applications:
  - name: "Flaky"
    source: winget
    optional: true
    requires_admin: true
    package:
      winget: { id: "Vendor.Flaky" }
  - name: "Normal"
    source: winget
    package:
      winget: { id: "Vendor.Normal" }
`)

	cfg, err := LoadWithImports(path)
	if err != nil {
		t.Fatalf("LoadWithImports: %v", err)
	}
	if !cfg.Applications[0].Optional || !cfg.Applications[0].RequiresAdmin {
		t.Errorf("flags not decoded: %+v", cfg.Applications[0])
	}
	if cfg.Applications[1].Optional || cfg.Applications[1].RequiresAdmin {
		t.Error("flags leaked to the application that did not declare them")
	}
}

func TestSourcesRoundTrip(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"source: winget\n", "winget\n"},
		{"source: [winget, scoop]\n", "- winget\n- scoop\n"},
	}

	for _, c := range cases {
		var doc struct {
			Source Sources `yaml:"source"`
		}
		if err := yaml.Unmarshal([]byte(c.in), &doc); err != nil {
			t.Fatalf("unmarshal %q: %v", c.in, err)
		}
		out, err := yaml.Marshal(doc.Source)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(out) != c.want {
			t.Errorf("round trip of %q = %q, want %q", c.in, out, c.want)
		}
	}
}

func TestCategoriesAndFilters(t *testing.T) {
	cfg := &Config{Applications: []Application{
		{Name: "a", Category: "Dev", Source: Sources{SourceWinget}},
		{Name: "b", Source: Sources{SourceScoop, SourceWinget}},
		{Name: "c", Category: "Dev", Source: Sources{SourceGitHub}},
	}}

	cats := cfg.Categories()
	if len(cats) != 2 || cats[0] != "Dev" || cats[1] != UncategorizedLabel {
		t.Errorf("Categories() = %v", cats)
	}
	if got := cfg.InCategory("Dev"); len(got) != 2 {
		t.Errorf("InCategory(Dev) returned %d apps, want 2", len(got))
	}
	if !cfg.HasCategory("dev") {
		t.Error("HasCategory should be case-insensitive")
	}
	if got := cfg.FilterBySource(SourceWinget); len(got) != 2 {
		t.Errorf("FilterBySource(winget) returned %d apps, want 2", len(got))
	}
}

func TestPackagesID(t *testing.T) {
	p := Packages{
		Winget: &WingetSpec{ID: "Git.Git"},
		GitHub: &GitHubSpec{ID: "cli/cli"},
	}

	if id, ok := p.ID(SourceWinget); !ok || id != "Git.Git" {
		t.Errorf("ID(winget) = %q, %v", id, ok)
	}
	if id, ok := p.ID("choco"); ok {
		t.Errorf("ID(choco) = %q, want not configured", id)
	}
	if got := p.Configured(); len(got) != 2 {
		t.Errorf("Configured() = %v, want winget and github", got)
	}
}
