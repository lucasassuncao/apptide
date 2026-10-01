package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ErrLocked is returned when the state lock could not be acquired before the
// timeout elapsed.
var ErrLocked = errors.New("state file is locked by another apptide process")

// StaleAfter is how old a lock file must be before it is treated as abandoned
// and stolen. A process killed mid-install leaves its lock behind; without a
// steal window the user would have to delete the file by hand.
const StaleAfter = 5 * time.Minute

// Unlock releases a lock acquired with Lock. It is safe to call more than once.
type Unlock func()

// Lock acquires an advisory lock for the state file at path, retrying until
// timeout. Two apptide processes can legitimately run at once — an install in
// one terminal, a TUI in another — and the last writer would otherwise discard
// the other's bindings.
//
// The lock is a sibling file created with O_EXCL, holding the owning PID. It is
// advisory: it only guards code paths that call Lock.
func Lock(path string, timeout time.Duration) (Unlock, error) {
	lockPath := path + ".lock"
	deadline := time.Now().Add(timeout)

	// The state directory does not exist before the first save, and the lock is
	// taken before anything writes. Without this, a fresh machine fails the very
	// first run with "the system cannot find the path specified".
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o750); err != nil {
		return nil, fmt.Errorf("creating state dir for lock %q: %w", lockPath, err)
	}

	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //#nosec G304 -- sibling of the state file this package already owns
		if err == nil {
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
			_ = f.Close()

			var once bool
			return func() {
				if once {
					return
				}
				once = true
				_ = os.Remove(lockPath)
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquiring state lock %q: %w", lockPath, err)
		}

		if stale(lockPath) {
			// Best effort: if the steal loses a race, the next iteration
			// simply finds a fresh lock and keeps waiting.
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, lockPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// stale reports whether the lock file is old enough to be considered abandoned.
func stale(lockPath string) bool {
	fi, err := os.Stat(lockPath)
	if err != nil {
		// Vanished between the failed create and now: treat as not stale and
		// let the next create attempt succeed.
		return false
	}
	return time.Since(fi.ModTime()) > StaleAfter
}
