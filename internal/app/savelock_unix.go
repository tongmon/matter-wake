//go:build unix

package app

import (
	"errors"
	"os"
	"syscall"
)

// lockExclusive opens path, creating it, and takes an exclusive flock on
// it without waiting, so no other flock on it succeeds until unlock or the
// process ends. Every open of the file counts apart, so even a second open
// in this process is refused.
func lockExclusive(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
