//go:build unix

package lockfile

import (
	"errors"
	"os"
	"syscall"
)

// tryLock locks the file exclusively, without waiting. It tells if it is locked. (The file is opened with O_CLOEXEC,
// so the processes that runtree starts do not inherit the lock.)
func tryLock(f *os.File) (bool, error) {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, syscall.EWOULDBLOCK):
			return false, nil
		case errors.Is(err, syscall.EINTR):
			continue
		default:
			return false, err
		}
	}
}
