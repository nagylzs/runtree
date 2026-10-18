//go:build unix

package lockfile

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// The processes that are started meanwhile do not keep the lock after it is released
func TestNotInherited(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tree.lock")
	l, err := Acquire(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "5")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	_ = l.Release()
	l2, err := Acquire(path, "again")
	if err != nil {
		t.Fatalf("the child holds the lock: %v", err)
	}
	_ = l2.Release()
}
