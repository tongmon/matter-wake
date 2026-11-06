package app

import (
	"errors"
	"syscall"
)

// errorSharingViolation is ERROR_SHARING_VIOLATION: another handle has the
// file open without sharing it.
const errorSharingViolation syscall.Errno = 32

// lockExclusive opens path, creating it, without sharing it with any other
// handle, so no other open succeeds until unlock or the process ends.
func lockExclusive(path string) (unlock func(), err error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if errors.Is(err, errorSharingViolation) {
		return nil, errLocked
	}
	if err != nil {
		return nil, err
	}
	return func() { _ = syscall.CloseHandle(h) }, nil
}
