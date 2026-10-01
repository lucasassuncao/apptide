package installer

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestTailWriterKeepsOnlyTheEnd(t *testing.T) {
	var w tailWriter
	// Write more than the limit, one line at a time, and check the tail
	// survives while the head is dropped.
	for _, chunk := range []string{
		"first line that should be dropped\n",
		strings.Repeat("padding line\n", 500),
		"the actual error\n",
	} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}

	got := w.String()
	if strings.Contains(got, "first line") {
		t.Error("the head of the output was kept instead of the tail")
	}
	if !strings.Contains(got, "the actual error") {
		t.Errorf("the tail was lost: %q", got)
	}
	if w.buf.Len() > outputLimit {
		t.Errorf("buffer grew to %d bytes, limit is %d", w.buf.Len(), outputLimit)
	}
}

func TestCondenseDropsBlanksAndProgressRedraws(t *testing.T) {
	got := condense("Installing...\r50%\r100%\n\n\nDone\n")

	if strings.Contains(got, "\n") || strings.Contains(got, "\r") {
		t.Errorf("condense left line breaks in: %q", got)
	}
	if !strings.Contains(got, "Done") {
		t.Errorf("condense dropped the final line: %q", got)
	}
}

func TestCommandErrorIncludesOutput(t *testing.T) {
	inner := errors.New("exit status 1")
	err := &CommandError{Name: "choco", Err: inner, Output: "package not found"}

	if !strings.Contains(err.Error(), "package not found") {
		t.Errorf("the reason is missing from %q", err)
	}
	if !strings.Contains(err.Error(), "choco") {
		t.Errorf("the command is missing from %q", err)
	}
	// Unwrapping has to keep working: the winget path matches on *exec.ExitError.
	if !errors.Is(err, inner) {
		t.Error("CommandError does not unwrap to the underlying error")
	}
}

func TestCommandErrorWithoutOutput(t *testing.T) {
	err := &CommandError{Name: "winget", Err: errors.New("exit status 1")}
	if strings.HasSuffix(err.Error(), ": ") {
		t.Errorf("empty output left a dangling separator: %q", err)
	}
}

// A failing command must surface what it printed, not just its exit code.
// "exit status 1" on its own was the entire explanation for every failed
// install.
func TestRunAttachesOutputToTheError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uses cmd.exe")
	}

	err := runCtx(context.Background(), "cmd", "/C", "echo something broke && exit 1")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "something broke") {
		t.Errorf("the command's output is missing from %q", err)
	}

	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) {
		t.Errorf("got %T, want *CommandError", err)
	}
}

func TestRunReturnsNilOnSuccess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uses cmd.exe")
	}
	if err := runCtx(context.Background(), "cmd", "/C", "exit 0"); err != nil {
		t.Errorf("a successful command returned %v", err)
	}
}

// Cancellation must stop the command rather than let it run on unwatched.
func TestRunHonoursContext(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uses cmd.exe")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runCtx(ctx, "cmd", "/C", "exit 0"); err == nil {
		t.Error("a cancelled context still ran the command to completion")
	}
}
