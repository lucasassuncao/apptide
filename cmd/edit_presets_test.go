package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
	"gopkg.in/yaml.v3"
)

// Every preset the editor can insert must be valid YAML that sets the block it
// is offered under — a preset that lands under the wrong key would silently
// produce a config missing the field the user thought they had filled in.
func TestBlockPresetsSetTheirField(t *testing.T) {
	for _, field := range AppTideBlockPresets.ListFields() {
		for _, name := range AppTideBlockPresets.ListPresets(field) {
			body, err := AppTideBlockPresets.PresetYAML(field, name)
			if err != nil {
				t.Errorf("%s/%s: %v", field, name, err)
				continue
			}
			var doc map[string]any
			if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
				t.Errorf("%s/%s: not valid YAML: %v\n%s", field, name, err, body)
				continue
			}
			if _, ok := doc[field]; !ok {
				t.Errorf("%s/%s: block does not set %q\n%s", field, name, field, body)
			}
		}
	}
}

// Presets must decode into the schema with no unknown keys: they are built
// from Go values, so a field renamed in config.go without updating the preset
// map is the failure this guards against.
func TestBlockPresetsDecodeStrictly(t *testing.T) {
	for _, field := range AppTideBlockPresets.ListFields() {
		for _, name := range AppTideBlockPresets.ListPresets(field) {
			body, err := AppTideBlockPresets.PresetYAML(field, name)
			if err != nil {
				t.Errorf("%s/%s: %v", field, name, err)
				continue
			}
			dec := yaml.NewDecoder(strings.NewReader(body))
			dec.KnownFields(true)
			var cfg config.Config
			if err := dec.Decode(&cfg); err != nil {
				t.Errorf("%s/%s: does not decode into config.Config: %v\n%s", field, name, err, body)
			}
		}
	}
}

// A preset must satisfy the rules the editor enforces on save; otherwise
// inserting one and pressing Ctrl+S reports errors the user did not write.
func TestBlockPresetsPassValidators(t *testing.T) {
	wired := wiredEditValidators(t)

	for _, field := range AppTideBlockPresets.ListFields() {
		for _, name := range AppTideBlockPresets.ListPresets(field) {
			body, err := AppTideBlockPresets.PresetYAML(field, name)
			if err != nil {
				t.Errorf("%s/%s: %v", field, name, err)
				continue
			}
			raw := []byte(presetScaffold(field) + body)
			if violations := runEditValidators(wired, raw); len(violations) > 0 {
				t.Errorf("%s/%s: %v\n%s", field, name, violations, raw)
			}
		}
	}
}

// The whole config a preset is inserted into must still load, imports and
// per-file defaults included.
func TestBlockPresetsLoad(t *testing.T) {
	for _, field := range AppTideBlockPresets.ListFields() {
		for _, name := range AppTideBlockPresets.ListPresets(field) {
			body, err := AppTideBlockPresets.PresetYAML(field, name)
			if err != nil {
				continue
			}
			path := filepath.Join(t.TempDir(), "packages.yaml")
			if err := os.WriteFile(path, []byte(presetScaffold(field)+body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := config.LoadWithImports(path); err != nil {
				t.Errorf("%s/%s: load: %v", field, name, err)
			}
		}
	}
}

// Unknown fields and preset names must error, never panic or return an empty
// snippet that would wipe the block it replaces.
func TestPresetLookupMisses(t *testing.T) {
	for _, field := range []string{"", "nope", "Applications", "settings "} {
		if _, err := AppTideBlockPresets.PresetYAML(field, "winget-app"); err == nil {
			t.Errorf("PresetYAML(%q, winget-app) returned nil error", field)
		}
		if got := AppTideBlockPresets.ListPresets(field); got != nil {
			t.Errorf("ListPresets(%q) = %v, want nil", field, got)
		}
	}
	for _, field := range AppTideBlockPresets.ListFields() {
		if _, err := AppTideBlockPresets.PresetYAML(field, "definitely-not-a-preset"); err == nil {
			t.Errorf("PresetYAML(%s, bogus) returned nil error", field)
		}
	}
}

// The document picker is backed by the same templates init writes; each one
// must load as a config, since it is offered as a starting point.
func TestDocPresetsMatchInitTemplates(t *testing.T) {
	names := AppTideDocPresets.ListPresets("")
	if len(names) == 0 {
		t.Fatal("no document templates offered")
	}
	if got, want := strings.Join(names, ","), strings.Join(initTemplateNames(), ","); got != want {
		t.Errorf("document templates = %s, init templates = %s", got, want)
	}

	for _, name := range names {
		body, err := AppTideDocPresets.PresetYAML("", name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		path := filepath.Join(t.TempDir(), "packages.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.LoadWithImports(path)
		if err != nil {
			t.Errorf("%s: load: %v\n%s", name, err, body)
			continue
		}
		if len(cfg.Applications) == 0 {
			t.Errorf("%s: template declares no applications", name)
		}
		if violations := runEditValidators(wiredEditValidators(t), []byte(body)); len(violations) > 0 {
			t.Errorf("%s: %v", name, violations)
		}
	}

	if _, err := AppTideDocPresets.PresetYAML("", "nope"); err == nil {
		t.Error("unknown document template returned nil error")
	}
	if _, err := AppTideDocPresets.PresetYAML("applications", "minimal"); err == nil {
		t.Error("document lookup under a named field returned nil error")
	}
	if got := AppTideDocPresets.ListPresets("applications"); got != nil {
		t.Errorf(`ListPresets("applications") = %v, want nil`, got)
	}
}

// presetScaffold supplies the required keys a preset block does not set
// itself, so the probe document is complete without duplicating the block
// under test.
func presetScaffold(field string) string {
	var b bytes.Buffer
	b.WriteString("schema_version: 2\n")
	if field != "applications" {
		b.WriteString("applications:\n  - name: probe\n    source: winget\n    package:\n      winget:\n        id: Probe.App\n")
	}
	return b.String()
}
