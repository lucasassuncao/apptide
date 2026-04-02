// Package pathutil manages the Windows user PATH environment variable.
package pathutil

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// IsInUserPath reports whether dir is already present in the current process PATH.
func IsInUserPath(dir string) bool {
	normalize := func(s string) string {
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(s), `/\`))
	}
	want := normalize(dir)
	for _, p := range strings.Split(os.Getenv("PATH"), ";") {
		if normalize(p) == want {
			return true
		}
	}
	return false
}

// AddToUserPath appends dir to the user-level PATH in the Windows registry via
// PowerShell's [Environment]::SetEnvironmentVariable. The change persists across
// reboots but takes effect in new shells only.
func AddToUserPath(dir string) error {
	// Use PowerShell to safely append without duplicating.
	script := fmt.Sprintf(
		`$p = [Environment]::GetEnvironmentVariable('Path','User'); `+
			`if ($p -notlike '*%s*') { [Environment]::SetEnvironmentVariable('Path',"$p;%s",'User') }`,
		dir, dir,
	)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adding %q to PATH: %w", dir, err)
	}
	return nil
}
