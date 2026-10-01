package cmd

import "testing"

// verify binds the same variables as install, so the filters set there must
// reach its runner too.
func TestVerifyPassesTheFiltersThrough(t *testing.T) {
	useConfig(t, sampleConfig)

	selectFlags(t, "", "no-such-tag")
	if err := verifyCmd.RunE(verifyCmd, nil); err != nil {
		t.Errorf("verify with nothing selected: %v", err)
	}

	selectFlags(t, "Nope")
	if err := verifyCmd.RunE(verifyCmd, nil); err == nil {
		t.Error("verify accepted a category the config does not have")
	}
}
