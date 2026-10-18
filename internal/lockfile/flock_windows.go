//go:build windows

package lockfile

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockOffset is the offset of the locked byte: far after the content, because a Windows lock forbids reading the
// locked bytes, and the others read who holds the lock
const lockOffset = 0x7fffffff_00000000

// tryLock locks the file exclusively, without waiting. It tells if it is locked. Windows releases the lock when the
// process exits. (Go opens the files without inheritance, so the processes that runtree starts do not get it.)
func tryLock(f *os.File) (bool, error) {
	ol := &windows.Overlapped{Offset: uint32(lockOffset & 0xffffffff), OffsetHigh: uint32(lockOffset >> 32)}
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, ol)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return false, nil
	}
	return false, err
}
