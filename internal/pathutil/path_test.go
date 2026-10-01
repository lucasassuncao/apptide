package pathutil

import (
	"os"
	"testing"
)

func TestIsInUserPath(t *testing.T) {
	// PATH entries vary in trailing separators and case, and a directory that
	// is already there must not be added a second time.
	t.Setenv("PATH", `C:\tools\bin;C:\Users\me\AppData\Local\apptide\bin\;C:\Windows`)

	tests := map[string]bool{
		`C:\tools\bin`:                              true,
		`c:\TOOLS\BIN`:                              true,
		`C:\Users\me\AppData\Local\apptide\bin`:     true,
		`C:\Users\me\AppData\Local\apptide\bin\`:    true,
		`C:\Users\me\AppData\Local\apptide\bin\sub`: false,
		`C:\elsewhere`:                              false,
	}

	for dir, want := range tests {
		if got := IsInUserPath(dir); got != want {
			t.Errorf("IsInUserPath(%q) = %v, want %v", dir, got, want)
		}
	}
}

func TestIsInUserPathWithEmptyEnvironment(t *testing.T) {
	t.Setenv("PATH", "")
	if IsInUserPath(`C:\tools`) {
		t.Error("an empty PATH reported a directory as present")
	}
}

// An entry with surrounding spaces is still the same directory.
func TestIsInUserPathTrimsWhitespace(t *testing.T) {
	t.Setenv("PATH", ` C:\tools\bin ;C:\Windows`)
	if !IsInUserPath(`C:\tools\bin`) {
		t.Errorf("padded PATH entry not matched (PATH=%q)", os.Getenv("PATH"))
	}
}
