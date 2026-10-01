package installer

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// outputLimit is how much of a command's output is kept for its error message.
// The useful part of a package manager failure is at the end, and a full
// install log does not belong in a table cell.
const outputLimit = 4 << 10

// CommandError is a failed external command, carrying the tail of what it
// printed.
//
// Output used to be discarded because the TUI owns the display, which left
// "exit status 1" as the entire explanation for every failed install. Keeping
// it out of the terminal and in the error gives the reason without breaking
// the interface.
type CommandError struct {
	Name   string
	Err    error
	Output string
}

func (e *CommandError) Error() string {
	if e.Output == "" {
		return fmt.Sprintf("%s: %v", e.Name, e.Err)
	}
	return fmt.Sprintf("%s: %v: %s", e.Name, e.Err, e.Output)
}

func (e *CommandError) Unwrap() error { return e.Err }

// tailWriter keeps only the last outputLimit bytes written to it.
type tailWriter struct {
	buf bytes.Buffer
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	if excess := w.buf.Len() - outputLimit; excess > 0 {
		w.buf.Next(excess)
	}
	return len(p), nil
}

// String returns the kept output as a single line, so it fits a status column.
func (w *tailWriter) String() string {
	return condense(w.buf.String())
}

// condense collapses output into one line, dropping blank lines and the
// progress bars package managers redraw with carriage returns.
func condense(s string) string {
	s = strings.ReplaceAll(s, "\r", "\n")
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			kept = append(kept, line)
		}
	}
	// The end of the output is where the reason is; the start is banners.
	if len(kept) > 6 {
		kept = kept[len(kept)-6:]
	}
	return strings.Join(kept, "; ")
}

// run executes a command, capturing its output. Nothing reaches the terminal:
// the caller owns the display, and the output comes back inside the error.
func run(ctx context.Context, name string, args ...string) (string, error) {
	var out tailWriter
	cmd := exec.CommandContext(ctx, name, args...) //#nosec G204 -- name is a package manager constant, args are built by this package
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	text := out.String()
	if err != nil {
		return text, &CommandError{Name: name, Err: err, Output: text}
	}
	return text, nil
}

// runCtx executes a command and reports failure with the output attached.
func runCtx(ctx context.Context, name string, args ...string) error {
	_, err := run(ctx, name, args...)
	return err
}
