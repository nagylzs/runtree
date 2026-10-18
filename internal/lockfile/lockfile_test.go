package lockfile

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const holdEnv = "LOCKFILE_TEST_HOLD"

// When the test binary is started with holdEnv, it holds the lock of that file until it is killed
func TestMain(m *testing.M) {
	if path := os.Getenv(holdEnv); path != "" {
		if _, err := Acquire(path, "the helper"); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("locked")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestAcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tree.lock")
	l, err := Acquire(path, "gandalf@host since 10:30, tree /trees/upgrade.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%d\ngandalf@host since 10:30, tree /trees/upgrade.yaml\n", os.Getpid())
	if content, _ := os.ReadFile(path); string(content) != want {
		t.Errorf("the content is %q", content)
	}
	// another open file, like another process
	_, err = Acquire(path, "other")
	var locked *LockedError
	if !errors.As(err, &locked) || locked.Holder != want {
		t.Fatalf("the error is %v", err)
	}
	if got := err.Error(); got != fmt.Sprintf("%s is locked, the lock file says: pid %d, gandalf@host since 10:30, "+
		"tree /trees/upgrade.yaml", path, os.Getpid()) {
		t.Errorf("the message is %q", got)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := Acquire(path, "second")
	if err != nil {
		t.Fatalf("after the release: %v", err)
	}
	_ = l2.Release()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the lock file is deleted: %v", err)
	}
}

// The lock is released when its holder is killed, the stale content does not matter
func TestReleasedWhenKilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tree.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), holdEnv+"="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "locked\n" {
		t.Fatalf("the helper says %q", line)
	}
	_, err = Acquire(path, "test")
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("pid %d, the helper", cmd.Process.Pid)) {
		t.Fatalf("the error is %v", err)
	}
	_ = cmd.Process.Kill() // SIGKILL
	_ = cmd.Wait()
	l, err := Acquire(path, "test")
	if err != nil {
		t.Fatalf("after the kill: %v", err)
	}
	_ = l.Release()
}

// A file that the user cannot write can be locked too, its content stays
func TestReadOnlyFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write the file")
	}
	path := filepath.Join(t.TempDir(), "tree.lock")
	if err := os.WriteFile(path, []byte("1\nsomeone\n"), 0444); err != nil {
		t.Fatal(err)
	}
	l, err := Acquire(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if content, _ := os.ReadFile(path); string(content) != "1\nsomeone\n" {
		t.Errorf("the content is %q", content)
	}
	if _, err := Acquire(path, "other"); err == nil {
		t.Error("it is not locked")
	}
}
