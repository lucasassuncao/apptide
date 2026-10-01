package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lucasassuncao/yedit/spec"
	"gopkg.in/yaml.v3"
)

// TestNewMetadata guards the editor's hint tree: metadata.New walks the schema
// with reflection and fails at runtime if a Metadata() map names a field that
// does not exist, which would otherwise only surface when a user runs
// `apptide edit`.
func TestNewMetadata(t *testing.T) {
	src, err := NewMetadata()
	if err != nil {
		t.Fatalf("NewMetadata: %v", err)
	}
	if src == nil {
		t.Fatal("NewMetadata returned a nil source")
	}
}

// The hint tree is also the rule set: the FromMetadata validators read the
// same nodes the hint panel shows. This guards the contract they depend on —
// every constraint declared in internal/config/metadata.go must resolve
// through FieldMeta at the path the editor asks for.
// schemaPaths lists every yaml key under t as block.field.path.
func schemaPaths(t reflect.Type, prefix string) []string {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var out []string
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		if prefix != "" {
			name = prefix + "." + name
		}
		out = append(append(out, name), schemaPaths(t.Field(i).Type, name)...)
	}
	return out
}

// A field without a description shows an empty hint panel, and a broken
// example teaches a config that does not load.
func TestEveryFieldHasAHint(t *testing.T) {
	src, err := NewMetadata()
	if err != nil {
		t.Fatalf("NewMetadata: %v", err)
	}
	for _, p := range schemaPaths(reflect.TypeFor[Config](), "") {
		block, rest, _ := strings.Cut(p, ".")
		m := src.FieldMeta(block, rest)
		if m.Description == "" {
			t.Errorf("%s has no description", p)
		}
		var v any
		if err := yaml.Unmarshal([]byte(m.Example), &v); err != nil {
			t.Errorf("%s example is not YAML: %v", p, err)
		}
	}
}

func TestEditHintsReachFieldMeta(t *testing.T) {
	src, err := NewMetadata()
	if err != nil {
		t.Fatalf("NewMetadata: %v", err)
	}

	required := []struct{ block, path string }{
		// Declared required by the schema even though the edit command hides
		// it (see editHiddenKeys): validate and the docs read this tree too.
		{"schema_version", ""},
		{"applications", ""},
		{"applications", "name"},
		{"applications", "source"},
		{"applications", "package"},
		{"applications", "package.winget.id"},
		{"applications", "package.chocolatey.id"},
		{"applications", "package.scoop.id"},
		{"applications", "package.github.id"},
	}
	for _, f := range required {
		if !src.FieldMeta(f.block, f.path).Required {
			t.Errorf("FieldMeta(%q, %q).Required = false, want true", f.block, f.path)
		}
	}

	optional := []struct{ block, path string }{
		{"applications", "description"},
		{"applications", "category"},
		{"applications", "version"},
		{"applications", "hooks"},
		{"applications", "hooks.post_install"},
		{"settings", "install_dir"},
		{"defaults", "category"},
	}
	for _, f := range optional {
		if src.FieldMeta(f.block, f.path).Required {
			t.Errorf("FieldMeta(%q, %q).Required = true, want false", f.block, f.path)
		}
	}

	actions := []string{"install", "uninstall", "skip"}
	oneOf := []struct {
		block, path string
		want        []string
	}{
		{"applications", "action", actions},
		{"defaults", "action", actions},
		{"applications", "package.winget.scope", []string{"machine", "user"}},
		{"applications", "package.scoop.arch", []string{"64bit", "32bit", "arm64"}},
	}
	for _, f := range oneOf {
		if got := src.FieldMeta(f.block, f.path).OneOf; !slices.Equal(got, f.want) {
			t.Errorf("FieldMeta(%q, %q).OneOf = %v, want %v", f.block, f.path, got, f.want)
		}
	}

	// owner/repo is enforced by pattern, and info_url by format: both only
	// fire when the metadata reaches the validator.
	if got := src.FieldMeta("applications", "package.github.id").Pattern; got == "" {
		t.Error("package.github.id has no pattern")
	}
	if got := src.FieldMeta("applications", "info_url").Formats; len(got) == 0 || got[0].Label() != spec.FormatURL.Label() {
		t.Errorf("info_url.Formats = %v, want [%s]", got, spec.FormatURL.Label())
	}
	if !src.FieldMeta("applications", "tags").Unique {
		t.Error("tags.Unique = false, want true")
	}
	if got := src.FieldMeta("applications", "").MinCount; got != 1 {
		t.Errorf("applications.MinCount = %d, want 1", got)
	}
}
