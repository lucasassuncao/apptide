package cmd

import (
	"strings"
	"testing"
)

// selectFlags sets the filter flags install, verify and tui share, and
// restores them when the test ends.
func selectFlags(t *testing.T, cat string, tags ...string) {
	t.Helper()
	prevCat, prevTags := category, tagFilter
	t.Cleanup(func() { category, tagFilter = prevCat, prevTags })
	category, tagFilter = cat, tags
}

// The filters reach the runner: a tag nothing carries selects nothing, so
// the run ends before any package manager is touched.
func TestInstallPassesTheFiltersThrough(t *testing.T) {
	useConfig(t, sampleConfig)
	selectFlags(t, "", "no-such-tag")

	out := captureCmdStdout(t, func() {
		if err := installCmd.RunE(installCmd, nil); err != nil {
			t.Errorf("install: %v", err)
		}
	})
	if !strings.Contains(out, "no applications matched") {
		t.Errorf("output = %q, want the nothing-matched notice", out)
	}
}

func TestInstallRejectsAnUnknownCategory(t *testing.T) {
	useConfig(t, sampleConfig)
	selectFlags(t, "Nope")

	if err := installCmd.RunE(installCmd, nil); err == nil {
		t.Error("install accepted a category the config does not have")
	}
}
