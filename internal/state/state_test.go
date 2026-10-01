package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tempStatePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state.json")
}

func TestKey(t *testing.T) {
	cases := map[string]string{
		"VS Code":             "vs-code",
		"  GitHub   CLI ":     "github-cli",
		"neovim":              "neovim",
		"Docker Desktop":      "docker-desktop",
		"\tTerraform\n":       "terraform",
		"Microsoft.PowerToys": "microsoft.powertoys",
	}
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadMissingFileYieldsEmptyState(t *testing.T) {
	path := tempStatePath(t)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if s.Len() != 0 {
		t.Errorf("Len() = %d, want 0", s.Len())
	}
	if s.Path() != path {
		t.Errorf("Path() = %q, want %q", s.Path(), path)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := tempStatePath(t)

	s := New(path)
	s.Bind("Neovim", Entry{
		InstalledVia: "scoop",
		PackageID:    "neovim",
		Version:      "0.10.2",
		Attempts: []Attempt{
			{Source: "winget", Result: ResultNotFound},
			{Source: "scoop", Result: ResultOK},
		},
	})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e, ok := got.Get("Neovim")
	if !ok {
		t.Fatal("entry not found after round trip")
	}
	if e.InstalledVia != "scoop" || e.Version != "0.10.2" {
		t.Errorf("entry = %+v, want scoop/0.10.2", e)
	}
	if len(e.Attempts) != 2 || e.Attempts[0].Result != ResultNotFound {
		t.Errorf("attempts = %+v, want the winget/scoop chain", e.Attempts)
	}
	if e.InstalledAt.IsZero() {
		t.Error("InstalledAt not set on first bind")
	}
}

func TestGetAndBindNormalizeKeys(t *testing.T) {
	s := New(tempStatePath(t))
	s.Bind("VS   Code", Entry{InstalledVia: "winget"})

	if _, ok := s.Get("vs-code"); !ok {
		t.Error("Get by normalized key failed")
	}
	if _, ok := s.Get("VS Code"); !ok {
		t.Error("Get by display name failed")
	}
	if keys := s.Keys(); len(keys) != 1 || keys[0] != "vs-code" {
		t.Errorf("Keys() = %v, want [vs-code]", keys)
	}
}

func TestBindPreservesInstalledAtAndAdvancesUpdatedAt(t *testing.T) {
	s := New(tempStatePath(t))
	first := time.Now().UTC().Add(-48 * time.Hour)
	s.Bind("Neovim", Entry{InstalledVia: "winget", InstalledAt: first})

	s.Bind("Neovim", Entry{InstalledVia: "scoop"})

	e, _ := s.Get("Neovim")
	if !e.InstalledAt.Equal(first) {
		t.Errorf("InstalledAt = %v, want preserved %v", e.InstalledAt, first)
	}
	if !e.UpdatedAt.After(first) {
		t.Errorf("UpdatedAt = %v, want after %v", e.UpdatedAt, first)
	}
	if e.InstalledVia != "scoop" {
		t.Errorf("InstalledVia = %q, want scoop", e.InstalledVia)
	}
}

func TestAdopt(t *testing.T) {
	s := New(tempStatePath(t))

	if err := s.Adopt("Ghost", "winget"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Adopt on unknown app: got %v, want ErrNotFound", err)
	}

	s.Bind("Neovim", Entry{InstalledVia: "scoop"})
	if err := s.Adopt("Neovim", "winget"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if e, _ := s.Get("Neovim"); e.InstalledVia != "winget" {
		t.Errorf("InstalledVia = %q, want winget", e.InstalledVia)
	}
}

func TestRemove(t *testing.T) {
	s := New(tempStatePath(t))
	s.Bind("Neovim", Entry{InstalledVia: "scoop"})

	if !s.Remove("Neovim") {
		t.Error("Remove reported no entry for a bound app")
	}
	if s.Remove("Neovim") {
		t.Error("Remove reported an entry on the second call")
	}
	if s.Len() != 0 {
		t.Errorf("Len() = %d, want 0", s.Len())
	}
}

func TestLoadCorruptFile(t *testing.T) {
	path := tempStatePath(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	var corrupt *CorruptError
	if _, err := Load(path); !errors.As(err, &corrupt) {
		t.Fatalf("Load on corrupt file: got %v, want *CorruptError", err)
	}
}

func TestLoadOrResetQuarantinesCorruptFile(t *testing.T) {
	path := tempStatePath(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, quarantined, err := LoadOrReset(path)
	if err != nil {
		t.Fatalf("LoadOrReset: %v", err)
	}
	if s.Len() != 0 {
		t.Errorf("Len() = %d, want an empty state", s.Len())
	}
	if quarantined == "" || !strings.Contains(quarantined, ".corrupt.") {
		t.Errorf("quarantined = %q, want a .corrupt. path", quarantined)
	}
	if _, err := os.Stat(quarantined); err != nil {
		t.Errorf("quarantined file missing: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("corrupt file still in place after quarantine")
	}
}

func TestLoadRejectsNewerSchema(t *testing.T) {
	path := tempStatePath(t)
	data, _ := json.Marshal(map[string]any{"version": Version + 1, "apps": map[string]any{}})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted a newer schema version")
	}
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := New(path)
	for _, name := range []string{"Neovim", "Git", "ripgrep"} {
		s.Bind(name, Entry{InstalledVia: "winget"})
		if err := s.Save(); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("dir contains %v, want only state.json", names)
	}
}

func TestSaveCreatesMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "state.json")

	s := New(path)
	s.Bind("Git", Entry{InstalledVia: "winget"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("state not written: %v", err)
	}
}

func TestSaveWithoutPath(t *testing.T) {
	s := &State{Version: Version, Apps: map[string]Entry{}}
	if err := s.Save(); err == nil {
		t.Fatal("Save succeeded without a path")
	}
}
