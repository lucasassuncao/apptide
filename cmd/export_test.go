package cmd

import "testing"

// -o is the global table|json flag. A local --output shadowed it, and
// `apptide export -o json` wrote a file named "json".
func TestExportDoesNotShadowTheOutputFlag(t *testing.T) {
	if exportCmd.LocalNonPersistentFlags().Lookup("output") != nil {
		t.Error("export declares its own --output")
	}
	if exportCmd.Flags().Lookup("out") == nil {
		t.Error("export has no --out flag")
	}
	if f := exportCmd.Flags().ShorthandLookup("o"); f == nil || f.Name != "output" {
		t.Errorf("-o on export resolves to %v, want the global --output", f)
	}
}
