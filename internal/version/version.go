// Package version tells the version of the programs of runtree. scripts/build.py sets the variables with -ldflags -X;
// a plain go build (or go install) takes them from the build information of Go: the version of the main module from
// its git tag (Go 1.24 and later), and the commit.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// Version is the version of the build: a git tag (v0.1.0), or the tag and the commits after it (v0.1.0-5-g1be6ff0,
// git describe), with -dirty when it had uncommitted changes. It is "devel" when it is not known.
var Version string

// Commit is the hash of the git commit of the build
var Commit string

// Branch is the git branch of the build (only with build.py)
var Branch string

// Built is the time of the build (build.py), or of the commit (go build), in RFC 3339
var Built string

func init() {
	fillFromBuildInfo()
}

// fillFromBuildInfo sets the variables that -ldflags did not set from the build information of Go
func fillFromBuildInfo() {
	bi, ok := debug.ReadBuildInfo()
	if ok {
		if Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			Version = bi.Main.Version
		}
		modified := false
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if Commit == "" {
					Commit = s.Value
				}
			case "vcs.time":
				if Built == "" {
					Built = s.Value
				}
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
		if modified && Version != "" && !strings.Contains(Version, "dirty") {
			Version += "+dirty"
		}
	}
	if Version == "" {
		Version = "devel"
	}
}

// BuiltTime is the time of the build, or nil when it is not known
func BuiltTime() *time.Time {
	res, err := time.Parse(time.RFC3339, Built)
	if err != nil {
		return nil
	}
	return &res
}

// ShortCommit is the first 12 characters of the commit
func ShortCommit() string {
	if len(Commit) > 12 {
		return Commit[:12]
	}
	return Commit
}

// String is the version with the details: "v0.1.0 (commit 1be6ff0e93e1, main, built 2026-10-21T17:12:09+02:00)"
func String() string {
	var details []string
	if c := ShortCommit(); c != "" {
		details = append(details, "commit "+c)
	}
	if Branch != "" {
		details = append(details, Branch)
	}
	if Built != "" {
		details = append(details, "built "+Built)
	}
	if len(details) == 0 {
		return Version
	}
	return Version + " (" + strings.Join(details, ", ") + ")"
}

// LocalVersion is String, e.g. for the runs of the log database
func LocalVersion() string {
	return String()
}

// PrintVersion prints the name of the program, its version, and the version of Go (--version)
func PrintVersion(program string) {
	fmt.Printf("%s %s\n%s %s/%s\n", program, String(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
