package cmd

import (
	"strings"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/yedit/document"
	"github.com/lucasassuncao/yedit/editor"
	"github.com/lucasassuncao/yedit/spec"
)

// wiredEditValidators prepares AppTideValidators exactly as the edit command
// does, so the FromMetadata rules see the same schema and hint tree they see
// at save time.
func wiredEditValidators(t *testing.T) editor.WiredValidators {
	t.Helper()

	hints, err := config.NewMetadata()
	if err != nil {
		t.Fatalf("NewMetadata: %v", err)
	}
	return editor.Wire(AppTideValidators, editor.Config{
		Schema:          &config.Config{},
		Metadata:        hints,
		Hidden:          editHiddenKeys,
		PassthroughKeys: editPassthroughKeys,
	})
}

func runEditValidators(wired editor.WiredValidators, raw []byte) []spec.Violation {
	return editor.RunAll(wired, raw, nil)
}

// The example config ships as the documented starting point: it must survive
// the rules the editor enforces on save.
func TestAppTideValidatorsAcceptExampleConfig(t *testing.T) {
	doc, err := document.Load("../examples/packages.yaml", nil)
	if err != nil {
		t.Fatalf("loading example config: %v", err)
	}
	if violations := editor.RunAll(wiredEditValidators(t), doc.Raw(), doc.Blocks()); len(violations) > 0 {
		t.Errorf("example config reports %d violations: %v", len(violations), violations)
	}
}

// The cross-field rules are the ones that cannot be expressed per field, so
// each is exercised against a config that breaks it.
func TestAppTideValidatorsCatchBrokenConfig(t *testing.T) {
	raw := []byte(`schema_version: 2
applications:
  - name: Duplicated
    source: [winget, winget]
    package:
      winget:
        id: Some.App
  - name: Duplicated
    source: [scoop, apt]
    package:
      scoop:
        id: some-app
  - name: Missing block
    source: [winget, chocolatey]
    package:
      winget:
        id: Other.App
  - name: Removed from github
    action: uninstall
    source: github
    package:
      github:
        id: owner/repo
`)

	want := []struct {
		path     string
		contains string
	}{
		{"applications[1].name", "duplicate value"},
		{"applications[0].source", "listed more than once"},
		{"applications[1].source", "not a valid source"},
		{"applications[2].package.chocolatey.id", "listed in source"},
		{"applications[3].action", "uninstall is not supported"},
	}

	got := runEditValidators(wiredEditValidators(t), raw)
	for _, w := range want {
		if !hasViolation(got, w.path, w.contains) {
			t.Errorf("no violation at %s containing %q; got %v", w.path, w.contains, got)
		}
	}
}

// The per-field rules come from internal/config/metadata.go: a missing name,
// source or package block must be caught without a rule written here.
func TestAppTideValidatorsCatchMissingRequiredFields(t *testing.T) {
	raw := []byte("schema_version: 2\napplications:\n  - description: nothing else\n")

	got := runEditValidators(wiredEditValidators(t), raw)
	for _, path := range []string{"applications[0].name", "applications[0].source", "applications[0].package"} {
		if !hasViolation(got, path, "") {
			t.Errorf("no violation at %s; got %v", path, got)
		}
	}
}

// The checksum pattern accepts what the installer verifies, so a mistyped
// digest is caught on save instead of failing the install.
func TestAppTideValidatorsCheckTheChecksumForm(t *testing.T) {
	config := func(sum string) []byte {
		return []byte(`schema_version: 2
applications:
  - name: tool
    source: github
    package:
      github:
        id: owner/repo
        checksum: "` + sum + `"
`)
	}
	path := "applications[0].package.github.checksum"
	wired := wiredEditValidators(t)

	digest := strings.Repeat("ab", 32)
	for _, ok := range []string{digest, "sha256:" + digest, strings.ToUpper(digest)} {
		if got := runEditValidators(wired, config(ok)); hasViolation(got, path, "") {
			t.Errorf("%q rejected: %v", ok, got)
		}
	}
	for _, bad := range []string{"abc", "md5:" + digest, digest + "00"} {
		if got := runEditValidators(wired, config(bad)); !hasViolation(got, path, "") {
			t.Errorf("%q accepted", bad)
		}
	}
}

// import: is resolved by the loader before the schema is decoded, so it must
// pass through the editor untouched rather than being reported as unknown.
func TestAppTideValidatorsAllowImportKey(t *testing.T) {
	raw := []byte("import:\n  - browsers.yaml\nschema_version: 2\napplications:\n  - name: probe\n    source: winget\n    package:\n      winget:\n        id: Probe.App\n")

	if violations := runEditValidators(wiredEditValidators(t), raw); len(violations) > 0 {
		t.Errorf("import: reported as a violation: %v", violations)
	}
}

// schema_version is hidden from the editor, so the editor must not demand it
// either: the rule and the UI read the same schema tree, and a required field
// with no control to fill it in would be an error the user cannot clear.
func TestAppTideValidatorsIgnoreSuppressedSchemaVersion(t *testing.T) {
	raw := []byte("applications:\n  - name: probe\n    source: winget\n    package:\n      winget:\n        id: Probe.App\n")

	if violations := runEditValidators(wiredEditValidators(t), raw); len(violations) > 0 {
		t.Errorf("a config without schema_version reports %v", violations)
	}

	// Without the Hidden filter the same document does report it, which is
	// what makes the assertion above evidence of the suppression rather than
	// of a missing rule.
	hints, err := config.NewMetadata()
	if err != nil {
		t.Fatalf("NewMetadata: %v", err)
	}
	unfiltered := editor.Wire(AppTideValidators, editor.Config{Schema: &config.Config{}, Metadata: hints})
	if !hasViolation(editor.RunAll(unfiltered, raw, nil), "schema_version", "") {
		t.Error("schema_version is not required by the unfiltered rules; this test no longer proves anything")
	}
}

func hasViolation(got []spec.Violation, path, contains string) bool {
	for _, v := range got {
		if v.Path == path && strings.Contains(v.Message, contains) {
			return true
		}
	}
	return false
}
