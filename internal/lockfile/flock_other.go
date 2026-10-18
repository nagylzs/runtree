//go:build !unix

package lockfile

import (
	"errors"
	"os"
)

// tryLock is not implemented on Windows (runtree does not build on Windows)
func tryLock(*os.File) (bool, error) {
	return false, errors.New("lock files are not supported on this system")
}
