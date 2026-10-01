//go:build windows

package elevation

import "testing"

// The answer depends on how the test runs, so only its stability is checked:
// the TUI reads it once at startup and trusts it for the whole session.
func TestIsElevatedIsStable(t *testing.T) {
	first := IsElevated()
	for range 3 {
		if IsElevated() != first {
			t.Fatal("IsElevated changed between calls in the same process")
		}
	}
}
