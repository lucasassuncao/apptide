package tui

import "testing"

// Status is read by colour at a glance: installed, pending and failed rows
// must not collapse into one look under the terminal theme.
func TestStatusStylesAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for name, st := range map[string]interface{ Render(...string) string }{
		"green": stGreen, "yellow": stYellow, "red": stRed,
	} {
		out := st.Render("x")
		if prev, dup := seen[out]; dup {
			t.Errorf("%s renders the same as %s: %q", name, prev, out)
		}
		seen[out] = name
	}
}
