package cmd

import "testing"

// The repo set at build time is the default, so a release binary updates
// itself without --repo.
func TestSelfUpdateDefaultsToTheBuildRepo(t *testing.T) {
	f := selfUpdateCmd.Flags().Lookup("repo")
	if f == nil {
		t.Fatal("self-update has no --repo flag")
	}
	if f.DefValue != DefaultRepo {
		t.Errorf("--repo default = %q, want DefaultRepo %q", f.DefValue, DefaultRepo)
	}
	if selfUpdateCmd.Flags().Lookup("github-token") == nil {
		t.Error("self-update has no --github-token flag")
	}
}
