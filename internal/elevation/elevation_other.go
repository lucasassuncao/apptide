//go:build !windows

package elevation

// IsElevated is Windows-specific; elsewhere apptide has no package managers to
// drive, so the answer is never used.
func IsElevated() bool { return false }
