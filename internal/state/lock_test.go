package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockIsExclusive(t *testing.T) {
	path := tempStatePath(t)

	unlock, err := Lock(path, time.Second)
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}

	if _, err := Lock(path, 150*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Lock: got %v, want ErrLocked", err)
	}

	unlock()
	unlock() // must be safe to call twice

	unlock2, err := Lock(path, time.Second)
	if err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	unlock2()
}

func TestLockStealsStaleLock(t *testing.T) {
	path := tempStatePath(t)
	lockPath := path + ".lock"
	if err := os.WriteFile(lockPath, []byte("4242"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * StaleAfter)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	unlock, err := Lock(path, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Lock did not steal a stale lock: %v", err)
	}
	unlock()
}

func TestLockReleaseRemovesFile(t *testing.T) {
	path := tempStatePath(t)

	unlock, err := Lock(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unlock()

	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Error("lock file still present after unlock")
	}
}

// The state directory does not exist before the first save, and Lock runs
// before anything writes. Without creating it, a fresh machine failed its very
// first `apptide install` with "the system cannot find the path specified".
func TestLockCreatesMissingStateDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptide", "state.json")

	unlock, err := Lock(path, time.Second)
	if err != nil {
		t.Fatalf("Lock in a non-existent directory: %v", err)
	}
	defer unlock()

	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Errorf("lock file was not created: %v", err)
	}
}

func TestLockIsExclusiveAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptide", "state.json")

	unlock, err := Lock(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// A second holder must not get in while the first has it.
	if _, err := Lock(path, 100*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Errorf("second Lock returned %v, want ErrLocked", err)
	}

	unlock()

	// And must succeed once it is released.
	second, err := Lock(path, time.Second)
	if err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	second()

	// Unlock is documented as safe to call more than once.
	unlock()
}
