package installer

import (
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
)

func TestResolveReturnsTheMatchingInstaller(t *testing.T) {
	tests := []struct {
		source config.Source
		want   string
	}{
		{config.SourceWinget, "winget"},
		{config.SourceChocolatey, "chocolatey"},
		{"choco", "chocolatey"}, // alias
		{config.SourceScoop, "scoop"},
		{config.SourceGitHub, "github"},
		{"WINGET", "winget"}, // case-insensitive
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			inst, err := Resolve(tt.source, Options{})
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tt.source, err)
			}
			if got := inst.Name(); got != tt.want {
				t.Errorf("Resolve(%q).Name() = %q, want %q", tt.source, got, tt.want)
			}
		})
	}

	if _, err := Resolve("apt", Options{}); err == nil {
		t.Error("Resolve should reject an unknown source")
	}
}

func TestResolveGitHubUsesTheGivenInstallDir(t *testing.T) {
	inst, err := Resolve(config.SourceGitHub, Options{DefaultInstallDir: `C:\custom\bin`})
	if err != nil {
		t.Fatal(err)
	}
	g, ok := inst.(*GitHub)
	if !ok {
		t.Fatalf("expected *GitHub, got %T", inst)
	}
	if g.installDir != `C:\custom\bin` {
		t.Errorf("installDir = %q, want C:\\custom\\bin", g.installDir)
	}

	// With no override, the default is used rather than an empty path.
	inst, err = Resolve(config.SourceGitHub, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if g := inst.(*GitHub); g.installDir == "" {
		t.Error("installDir should fall back to the default, not stay empty")
	}
}
