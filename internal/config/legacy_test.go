package config

import (
	"testing"
)

func TestLegacyV1IsConverted(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "old.yaml", `
Development:
  - name: "Git"
    source: winget
    description: "Version control"
    no_upgrade: true
    pre_install: "echo before"
    winget:
      id: "Git.Git"
      scope: machine

  - name: "Lazygit"
    source: github
    action: skip
    github:
      repo: "jesseduffield/lazygit"
      binary_name: "lazygit"

CLITools:
  - name: "jq"
    source: choco
    chocolatey:
      id: "jq"
`)

	cfg, err := LoadWithImports(path)
	if err != nil {
		t.Fatalf("LoadWithImports: %v", err)
	}
	if cfg.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1 for a legacy file", cfg.SchemaVersion)
	}
	if len(cfg.Applications) != 3 {
		t.Fatalf("got %d applications, want 3", len(cfg.Applications))
	}

	git := cfg.Applications[0]
	if git.Category != "Development" {
		t.Errorf("category = %q, want Development", git.Category)
	}
	if !git.SkipUpgrade {
		t.Error("no_upgrade did not become skip_upgrade")
	}
	if git.Pre(ActionInstall) != "echo before" {
		t.Errorf("pre_install did not move into hooks: %q", git.Pre(ActionInstall))
	}
	if git.Package.Winget == nil || git.Package.Winget.ID != "Git.Git" {
		t.Errorf("winget block lost: %+v", git.Package.Winget)
	}

	lazy := cfg.Applications[1]
	if lazy.Package.GitHub == nil || lazy.Package.GitHub.ID != "jesseduffield/lazygit" {
		t.Errorf("github.repo did not become package.github.id: %+v", lazy.Package.GitHub)
	}
	if lazy.EffectiveAction() != ActionSkip {
		t.Errorf("action = %q, want skip", lazy.EffectiveAction())
	}

	jq := cfg.Applications[2]
	if len(jq.Source) != 1 || jq.Source[0] != SourceChocolatey {
		t.Errorf("source alias 'choco' not normalized: %v", jq.Source)
	}
}

func TestLegacyFlatIDIsAdopted(t *testing.T) {
	dir := t.TempDir()
	// The pre-0135a20 layout put the id at the package level with no block.
	// It used to load without error and install nothing.
	path := writeFile(t, dir, "flat.yaml", `
Development:
  - name: "VS Code"
    source: winget
    id: "Microsoft.VisualStudioCode"
`)

	cfg, err := LoadWithImports(path)
	if err != nil {
		t.Fatalf("LoadWithImports: %v", err)
	}
	app := cfg.Applications[0]
	if app.Package.Winget == nil || app.Package.Winget.ID != "Microsoft.VisualStudioCode" {
		t.Fatalf("flat id was dropped: %+v", app.Package.Winget)
	}
}
