//go:build !unix && !windows

package lockfile

import (
	"errors"
	"os"
)

// tryLock is not implemented on the systems that are neither Unix nor Windows
func tryLock(*os.File) (bool, error) {
	return false, errors.New("lock files are not supported on this system")
}
