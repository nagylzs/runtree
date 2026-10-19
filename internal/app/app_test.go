package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nagylzs/runtree/internal/lockfile"
)

// locked tells if another process could not lock the file now (a lock of the same process works like another's:
// flock locks the opened file)
func locked(t *testing.T, path string) bool {
	t.Helper()
	l, err := lockfile.Acquire(path, "the test")
	var lockedErr *lockfile.LockedError
	if errors.As(err, &lockedErr) {
		return true
	}
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Release()
	return false
}

// LockTree locks the file of --lock and the lock of the root node (relative to the tree file), the same file once.
// When one of them is locked, the other one is released.
func TestLockTree(t *testing.T) {
	dir := t.TempDir()
	treeFile := filepath.Join(dir, "tree.rt.yaml")
	if err := os.WriteFile(treeFile, []byte("tree:\n  lock: tree.lock\n  args: [\"true\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tree, _, _, err := LoadTree(treeFile, "", 30)
	if err != nil {
		t.Fatal(err)
	}
	treeLock, other := filepath.Join(dir, "tree.lock"), filepath.Join(dir, "other.lock")

	release, err := LockTree(other, tree, treeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !locked(t, other) || !locked(t, treeLock) {
		t.Error("the files are not locked")
	}
	release()
	if locked(t, other) || locked(t, treeLock) {
		t.Error("the files are not released")
	}

	release, err = LockTree(treeLock, tree, treeFile) // the same file twice
	if err != nil {
		t.Fatal(err)
	}
	release()

	held, err := lockfile.Acquire(treeLock, "someone")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Release() }()
	var lockedErr *lockfile.LockedError
	if _, err := LockTree(other, tree, treeFile); !errors.As(err, &lockedErr) {
		t.Errorf("the error is %v", err)
	}
	if locked(t, other) {
		t.Error("the lock of --lock is not released")
	}
}
