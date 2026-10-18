package runner

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The rtrunner binaries that runtree uploads to the hosts of managed ssh runners (ssh://host without a path), by
// platform (goos-goarch): the embedded ones (runtree and runtree-headless built with -tags rtrunners, see
// internal/rtrunners), and the ones in the directory of SetBinaryDir, that override them.

// runnerBinary is an rtrunner for a platform
type runnerBinary struct {
	platform string // goos-goarch, e.g. linux-amd64
	hash     string // the SHA-256 of the binary, hex
	size     int
	load     func() ([]byte, error)
}

var binaries struct {
	lock     sync.Mutex
	embedded fs.FS  // rtrunner-<goos>-<goarch>.gz files
	dir      string // rtrunner-<goos>-<goarch> files
	loaded   map[string]*runnerBinary
	err      error
}

// SetEmbeddedBinaries sets the embedded rtrunner binaries: gzipped rtrunner-<goos>-<goarch>.gz files in fsys.
func SetEmbeddedBinaries(fsys fs.FS) {
	binaries.lock.Lock()
	defer binaries.lock.Unlock()
	binaries.embedded, binaries.loaded = fsys, nil
}

// SetBinaryDir sets a directory with rtrunner-<goos>-<goarch> files, they are uploaded instead of the embedded ones.
func SetBinaryDir(dir string) {
	binaries.lock.Lock()
	defer binaries.lock.Unlock()
	binaries.dir, binaries.loaded = dir, nil
}

// errNoBinaries is the error of a managed ssh runner when there is nothing to upload
var errNoBinaries = errors.New("this runtree has no rtrunner binaries to upload to the host (build it with " +
	"scripts/build.py, or use --rtrunner-dir), give the path of rtrunner on the host instead: " +
	"ssh://host/path/to/rtrunner")

// loadBinaries returns the binaries by platform. Their hashes are calculated once.
func loadBinaries() (map[string]*runnerBinary, error) {
	binaries.lock.Lock()
	defer binaries.lock.Unlock()
	if binaries.loaded != nil || binaries.err != nil {
		return binaries.loaded, binaries.err
	}
	res := make(map[string]*runnerBinary)
	add := func(platform string, load func() ([]byte, error)) error {
		content, err := load()
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		res[platform] = &runnerBinary{platform: platform, hash: hex.EncodeToString(sum[:]), size: len(content), load: load}
		return nil
	}
	if binaries.embedded != nil {
		names, err := fs.Glob(binaries.embedded, "rtrunner-*.gz")
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			fsys, name := binaries.embedded, name
			load := func() ([]byte, error) {
				f, err := fsys.Open(name)
				if err != nil {
					return nil, err
				}
				defer f.Close()
				r, err := gzip.NewReader(f)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", name, err)
				}
				return io.ReadAll(r)
			}
			platform := strings.TrimSuffix(strings.TrimPrefix(name, "rtrunner-"), ".gz")
			if err := add(platform, load); err != nil {
				binaries.err = fmt.Errorf("the embedded rtrunner %s: %w", platform, err)
				return nil, binaries.err
			}
		}
	}
	if binaries.dir != "" {
		names, err := filepath.Glob(filepath.Join(binaries.dir, "rtrunner-*"))
		if err != nil {
			return nil, err
		}
		for _, path := range names {
			platform := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "rtrunner-"), ".exe")
			if err := add(platform, func() ([]byte, error) { return os.ReadFile(path) }); err != nil {
				binaries.err = err
				return nil, err
			}
		}
	}
	binaries.loaded = res
	return res, nil
}

// unamePlatforms maps the output of uname -s and uname -m on the hosts to the platforms of the binaries
var unamePlatforms = []struct{ uname, platform string }{
	{"Linux/x86_64", "linux-amd64"},
	{"Linux/aarch64", "linux-arm64"},
	{"Linux/arm64", "linux-arm64"},
	{"Linux/armv6l", "linux-arm"},
	{"Linux/armv7l", "linux-arm"},
	{"Linux/armv8l", "linux-arm"}, // a 32 bit system on a 64 bit CPU
	{"Linux/i686", "linux-386"},
	{"Linux/i386", "linux-386"},
	{"Darwin/x86_64", "darwin-amd64"},
	{"Darwin/arm64", "darwin-arm64"},
}

// remoteDir is where the binaries are uploaded on the hosts, as rtrunner-<sha256>: a file with that name is always
// complete (it is renamed after checking it), and a newer runtree uploads its own one
const remoteDir = "$HOME/.cache/runtree"

// Exit codes of the start script
const (
	exitNotUploaded = 91 // the binary of the host is not uploaded yet
	exitNoPlatform  = 92 // there is no binary for the platform of the host
)

// The start script prints this to the standard error, with the platform of the host, when it exits with
// exitNotUploaded
const notUploadedMessage = "runtree: rtrunner is not uploaded yet for "

// startScript returns the shell script that starts the uploaded rtrunner --stdio of the platform of the host, or
// exits with exitNotUploaded (or exitNoPlatform). It is a single line without ' and ! (it is in single quotes for the
// login shell of the user, that can be csh too).
func startScript(bins map[string]*runnerBinary) string {
	var cases []string
	for _, up := range unamePlatforms {
		if b, ok := bins[up.platform]; ok {
			cases = append(cases, fmt.Sprintf("%s) p=%s h=%s ;;", up.uname, b.platform, b.hash))
		}
	}
	return strings.Join([]string{
		`case "$(uname -s)/$(uname -m)" in ` + strings.Join(cases, " ") +
			` *) echo "runtree has no rtrunner for the platform of the host: $(uname -s) $(uname -m)" >&2;` +
			fmt.Sprintf(" exit %d ;; esac", exitNoPlatform),
		`f="` + remoteDir + `/rtrunner-$h"`,
		fmt.Sprintf(`[ -x "$f" ] || { echo "%s$p" >&2; exit %d; }`, notUploadedMessage, exitNotUploaded),
		`touch "$f" 2>/dev/null`, // it is in use, see uploadScript
		`exec "$f" --stdio`,
	}, "; ")
}

// uploadScript returns the shell script that saves its standard input as the binary with the hash, after checking
// that it is complete. It deletes the binaries that were not used for 30 days. Like startScript, it is a single line
// without ' and !.
func uploadScript(hash string) string {
	return strings.Join([]string{
		"set -e",
		"umask 077",
		`d="` + remoteDir + `"`,
		`mkdir -p "$d"`,
		`t="$d/rtrunner-` + hash + `.tmp.$$"`,
		`trap "rm -f \"$t\"" EXIT`,
		`cat > "$t"`,
		`chmod 700 "$t"`,
		`"$t" --check-sha256 ` + hash,
		`mv -f "$t" "$d/rtrunner-` + hash + `"`,
		`find "$d" -name "rtrunner-*" -type f -mtime +30 -exec rm -f {} + 2>/dev/null || true`,
	}, "; ")
}

// platforms returns the platforms of the binaries, sorted
func platforms(bins map[string]*runnerBinary) []string {
	var res []string
	for p := range bins {
		res = append(res, p)
	}
	sort.Strings(res)
	return res
}

// CheckOwnSHA256 returns an error if the SHA-256 of the executable of the process is not want (hex), e.g. when the
// upload of rtrunner was incomplete (rtrunner --check-sha256)
func CheckOwnSHA256(want string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	content, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(content)
	if got := hex.EncodeToString(sum[:]); got != strings.ToLower(want) {
		return fmt.Errorf("the SHA-256 of %s is %s, not %s (the upload was not complete?)", exe, got, want)
	}
	return nil
}
