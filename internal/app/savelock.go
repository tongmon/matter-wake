package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// lockFile is the file a save folder is locked through.
const lockFile = "lock"

// errLocked is what lockExclusive returns when another holds the lock.
var errLocked = errors.New("locked")

// lockSave makes this process the only one with the save in dir open,
// until it calls unlock. Two sessions on one save would each save their
// own world over the other's.
//
// The operating system holds the lock on an open file and drops it when
// the process ends, crash or not, so a lock file left on disk locks
// nothing and no process ids are needed.
func lockSave(dir string) (unlock func(), err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("app: -save: %w", err)
	}
	unlock, err = lockExclusive(filepath.Join(dir, lockFile))
	switch {
	case errors.Is(err, errLocked):
		return nil, fmt.Errorf("app: %s is open in another matter-wake; close that one first", dir)
	case err != nil:
		return nil, fmt.Errorf("app: -save: lock: %w", err)
	}
	return unlock, nil
}
