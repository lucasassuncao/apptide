package cmd

import (
	"testing"
)

func TestResolveEditPaths(t *testing.T) {
	configHome(t)

	cases := []struct {
		name, config, out string
		load, save        string
	}{
		{"config and out", "a.yaml", "b.yaml", "a.yaml", "b.yaml"},
		{"config only", "a.yaml", "", "a.yaml", ""},
		// --out alone bootstraps that file instead of failing on a missing one.
		{"out only", "", "b.yaml", "b.yaml", ""},
		{"neither", "", "", DefaultConfigPath(), ""},
	}
	for _, c := range cases {
		load, save := resolveEditPaths(c.config, c.out)
		if load != c.load || save != c.save {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.name, load, save, c.load, c.save)
		}
	}
}

// Hiding a key removes it from the schema, so the value in the file would read
// as an unknown key and block the save unless the key is also declared
// passthrough.
func TestEditHiddenKeysArePassthrough(t *testing.T) {
	for _, hidden := range editHiddenKeys {
		found := false
		for _, k := range editPassthroughKeys {
			if k == hidden {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is hidden but not passthrough: its value would be reported as an unknown key", hidden)
		}
	}
}
