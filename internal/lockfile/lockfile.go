// Package lockfile is an exclusive lock on a file (runtree --lock), so that a tree is not run twice at the same time.
//
// It is a flock(2) lock: the kernel releases it when the process exits in any way (also when it is killed), so there
// are no stale locks, and it is atomic. The file is never deleted (deleting it would let two processes lock two
// different files with the same name). While the lock is held, the file has the pid of the holder on its first line
// (like a pid file), and who holds it on the second one.
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Lock is a held lock, it is released by Release or when the process exits
type Lock struct {
	f *os.File
}

// LockedError is the error of Acquire when another process holds the lock
type LockedError struct {
	Path   string
	Holder string // the content of the file: the pid and who holds the lock, as the holder wrote it
}

func (e *LockedError) Error() string {
	// a holder that cannot write the file leaves the content of an earlier holder there
	pid, who, _ := strings.Cut(strings.TrimSpace(e.Holder), "\n")
	switch {
	case pid == "":
		return fmt.Sprintf("%s is locked by another process", e.Path)
	case who == "":
		return fmt.Sprintf("%s is locked, the lock file says: pid %s", e.Path, pid)
	}
	return fmt.Sprintf("%s is locked, the lock file says: pid %s, %s", e.Path, pid, strings.TrimSpace(who))
}

// Acquire locks the file at path (it creates it when needed), and writes the pid of the process and holder into it
// (when the file is writable). It returns a *LockedError when another process holds the lock.
func Acquire(path string, holder string) (*Lock, error) {
	// Any user who can open the file can lock it, also when it can only be read (then the holder is not written).
	writable := true
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666)
	if errors.Is(err, os.ErrPermission) {
		writable = false
		f, err = os.Open(path)
	}
	if err != nil {
		return nil, err
	}
	locked, err := tryLock(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("could not lock %s: %w", path, err)
	}
	if !locked {
		content, _ := os.ReadFile(path)
		_ = f.Close()
		return nil, &LockedError{Path: path, Holder: string(content)}
	}
	if writable {
		content := fmt.Sprintf("%d\n%s\n", os.Getpid(), holder)
		if err := f.Truncate(0); err == nil {
			_, _ = f.WriteAt([]byte(content), 0)
		}
	}
	return &Lock{f: f}, nil
}

// Release releases the lock (the file stays)
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close() // closing the file releases the lock
	l.f = nil
	return err
}
