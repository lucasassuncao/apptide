package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
)

// initTo points init at a file in a temp dir until the test ends.
func initTo(t *testing.T, template string, force bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "packages.yaml")
	prevOut, prevTpl, prevForce := initOutputFile, initTemplate, initForce
	t.Cleanup(func() { initOutputFile, initTemplate, initForce = prevOut, prevTpl, prevForce })
	initOutputFile, initTemplate, initForce = path, template, force
	return path
}

// noErrors fails the test on any error validate would report for cfg.
func noErrors(t *testing.T, label string, cfg *config.Config) {
	t.Helper()
	for _, app := range cfg.Applications {
		for _, i := range validateApplication(app) {
			if i.severity == sevError {
				t.Errorf("%s: %s.%s: %s", label, i.app, i.field, i.message)
			}
		}
	}
}

// A template is the first config most users ever run: it has to load and
// pass apptide validate as written.
func TestInitTemplatesPassValidate(t *testing.T) {
	for _, name := range initTemplateNames() {
		path := initTo(t, name, false)
		captureCmdStdout(t, func() {
			if err := runInitTemplate(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		})

		cfg, err := config.LoadWithImports(path)
		if err != nil {
			t.Fatalf("%s does not load: %v", name, err)
		}
		if len(cfg.Applications) == 0 {
			t.Errorf("%s declares no application", name)
		}
		noErrors(t, name, cfg)
	}
}

func TestInitRejectsAnUnknownTemplate(t *testing.T) {
	initTo(t, "nope", false)
	if err := runInitTemplate(); err == nil || !strings.Contains(err.Error(), "minimal") {
		t.Errorf("err = %v, want one listing the valid templates", err)
	}
}

func TestInitDoesNotOverwriteWithoutForce(t *testing.T) {
	path := initTo(t, "minimal", false)
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := initCmd.RunE(initCmd, nil); err == nil {
		t.Fatal("init overwrote an existing file without --force")
	}
	if data, _ := os.ReadFile(path); string(data) != "keep me" {
		t.Errorf("file changed to %q", data)
	}
}

// The wizard writes YAML by hand, so what it writes must read back as the
// applications that were collected.
func TestWriteWizardResultLoadsBack(t *testing.T) {
	apps := []config.Application{
		{
			Name: "Git", Category: "Development", Description: `Say "hi"`,
			Source:  config.Sources{config.SourceWinget},
			Package: config.Packages{Winget: &config.WingetSpec{ID: "Git.Git"}},
		},
		{
			Name: "lazygit", Version: "v0.42.0", Action: config.ActionSkip,
			Source:  config.Sources{config.SourceGitHub},
			Package: config.Packages{GitHub: &config.GitHubSpec{ID: "jesseduffield/lazygit"}},
		},
	}

	path := filepath.Join(t.TempDir(), "packages.yaml")
	if err := writeWizardResult(path, apps); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadWithImports(path)
	if err != nil {
		t.Fatalf("wizard output does not load: %v", err)
	}
	if len(cfg.Applications) != 2 {
		t.Fatalf("got %d applications, want 2", len(cfg.Applications))
	}

	git, lazy := cfg.Applications[0], cfg.Applications[1]
	if git.Description != `Say "hi"` || git.Category != "Development" {
		t.Errorf("git = %+v", git)
	}
	if id, _ := lazy.Package.ID(config.SourceGitHub); id != "jesseduffield/lazygit" {
		t.Errorf("lazygit github id = %q", id)
	}
	if lazy.Version != "v0.42.0" || lazy.EffectiveAction() != config.ActionSkip {
		t.Errorf("lazygit = version %q, action %q", lazy.Version, lazy.EffectiveAction())
	}
	noErrors(t, "wizard", cfg)
}

func TestPackageIDLine(t *testing.T) {
	gh := config.Application{
		Version: "v1.0.0",
		Source:  config.Sources{config.SourceGitHub},
		Package: config.Packages{GitHub: &config.GitHubSpec{ID: "o/r"}},
	}
	if got := packageIDLine(gh); got != "id: o/r  @v1.0.0" {
		t.Errorf("github line = %q", got)
	}
	if got := packageIDLine(config.Application{Source: config.Sources{config.SourceWinget}}); got != "" {
		t.Errorf("an application without an id gave %q", got)
	}
}
