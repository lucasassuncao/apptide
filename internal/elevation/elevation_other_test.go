//go:build !windows

package elevation

import "testing"

func TestIsElevatedIsFalseOutsideWindows(t *testing.T) {
	if IsElevated() {
		t.Error("IsElevated() = true on a platform with no package managers to drive")
	}
}
