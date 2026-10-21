//go:build !windows

package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The options of the shared ssh connections, and their directory
func TestSSHShare(t *testing.T) {
	a, _ := ParseAddress("ssh://root@server01:2222")
	t.Cleanup(func() { SetSSHShare(false) })
	// not t.TempDir(): it is too long for a socket path on macOS (/var/folders/...)
	run, cache := shortTempDir(t), shortTempDir(t)
	// the user cache directory is $XDG_CACHE_HOME on Linux, $HOME/Library/Caches on macOS
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	userCache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		runtimeDir, dir string
	}{
		{run, filepath.Join(run, "runtree")},
		{"", filepath.Join(userCache, "runtree", "ssh")},  // e.g. macOS
		{filepath.Join(run, strings.Repeat("x", 80)), ""}, // the socket path would be too long
	} {
		t.Setenv("XDG_RUNTIME_DIR", tc.runtimeDir)
		SetSSHShare(true)
		got := strings.Join(sshOptions(a), " ")
		want := "-T -o BatchMode=yes -p 2222"
		if tc.dir != "" {
			want = "-T -o BatchMode=yes -o ControlMaster=auto -o ControlPath=" + tc.dir + "/%C -o ControlPersist=60 " +
				"-p 2222"
			if st, err := os.Stat(tc.dir); err != nil || st.Mode().Perm() != 0700 {
				t.Errorf("%s: %v, %v", tc.dir, st, err)
			}
		}
		if got != want {
			t.Errorf("XDG_RUNTIME_DIR=%s: got %s, want %s", tc.runtimeDir, got, want)
		}
	}
	SetSSHShare(false)
	if got := strings.Join(sshOptions(a), " "); got != "-T -o BatchMode=yes -p 2222" {
		t.Errorf("disabled: %s", got)
	}
}

// shortTempDir is a temporary directory in /tmp, short enough for socket paths in it
func shortTempDir(t *testing.T) string {
	dir, err := os.MkdirTemp("/tmp", "rt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
