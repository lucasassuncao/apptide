package runner

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/inventory"
)

// What export writes has to load back as a config, or the round trip it
// promises (export here, install there) breaks.
func TestWriteCategoryLoadsBackAsAConfig(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("schema_version: 2\n\napplications:\n")
	writeCategory(&buf, "Winget", config.SourceWinget, []inventory.Entry{
		{ID: "Git.Git", Version: "2.51.0"},
		{ID: "Some.Tool", Version: "Unknown"},
	})

	path := filepath.Join(t.TempDir(), "packages.yaml")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadWithImports(path)
	if err != nil {
		t.Fatalf("exported YAML does not load: %v\n%s", err, buf.String())
	}
	if len(cfg.Applications) != 2 {
		t.Fatalf("got %d applications, want 2", len(cfg.Applications))
	}

	git := cfg.Applications[0]
	if id, _ := git.Package.ID(config.SourceWinget); id != "Git.Git" {
		t.Errorf("winget id = %q, want Git.Git", id)
	}
	if git.Version != "2.51.0" || git.Category != "Winget" {
		t.Errorf("git = version %q, category %q", git.Version, git.Category)
	}
	// winget prints "Unknown" for versions it cannot read; pinning that would
	// make the config ask for a version that does not exist.
	if v := cfg.Applications[1].Version; v != "" {
		t.Errorf("an Unknown version was exported as %q", v)
	}
}

// With no manager on the machine the file still gets its header, so it is a
// valid starting point rather than an empty file.
func TestExportWithNoManagersWritesTheHeader(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	path := filepath.Join(t.TempDir(), "export.yaml")

	captureStdout(t, func() {
		if err := Export(ExportOptions{Output: path}); err != nil {
			t.Errorf("Export: %v", err)
		}
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), fmt.Sprintf("schema_version: %d", config.SchemaVersion)) || !strings.Contains(string(data), "applications:") {
		t.Errorf("header missing:\n%s", data)
	}
}
