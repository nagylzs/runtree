package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"time"

	"github.com/nagylzs/runtree/internal/lockfile"
)

// exitLocked is the exit code when the lock file is locked by another runtree (EX_TEMPFAIL: try again later)
const exitLocked = 75

// lockTree acquires the lock file of --lock for the run of the tree
func lockTree(path, treeFile string) (*lockfile.Lock, error) {
	userName, host := currentUser()
	holder := fmt.Sprintf("%s@%s since %s, tree %s", userName, host, time.Now().Format("2006-01-02 15:04:05 -0700"),
		treeFile)
	l, err := lockfile.Acquire(path, holder)
	if err != nil {
		return nil, err
	}
	slog.Info("holding the lock", "file", path)
	return l, nil
}

// currentUser returns the name of the user and the host
func currentUser() (string, string) {
	host, _ := os.Hostname()
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	return name, host
}
