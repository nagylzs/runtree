//go:build !windows

package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSSHScript is an ssh that runs the command on the local machine: it skips the options and the destination
const fakeSSHScript = `#!/bin/sh
while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done
shift 2
exec sh -c "$1"
`

// managedSetup makes a fake host for the managed ssh runners: HOME is a temporary directory, ssh runs the commands
// locally, and the binary of the platform is the test binary (it works as rtrunner, see TestMain). It returns the
// directory of the uploaded binaries on the "host", and the binary.
func managedSetup(t *testing.T, platform string) (string, *runnerBinary) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(asRtrunnerEnv, "1")
	ssh := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(ssh, []byte(fakeSSHScript), 0700); err != nil {
		t.Fatal(err)
	}
	setSSH(t, ssh)

	dir := t.TempDir()
	copyFile(t, os.Args[0], filepath.Join(dir, "rtrunner-"+platform))
	SetBinaryDir(dir)
	t.Cleanup(func() { SetBinaryDir("") })
	uploads.lock.Lock()
	uploads.m = make(map[string]*uploadCall)
	uploads.lock.Unlock()

	bins, err := loadBinaries()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".cache", "runtree"), bins[platform]
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	src, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
}

var localPlatform = runtime.GOOS + "-" + runtime.GOARCH

// eventRecorder records the event attributes of the log records
type eventRecorder struct {
	lock   sync.Mutex
	events []string
}

func (h *eventRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (h *eventRecorder) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *eventRecorder) WithGroup(string) slog.Handler            { return h }

func (h *eventRecorder) Handle(_ context.Context, r slog.Record) error {
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "event" {
			h.lock.Lock()
			h.events = append(h.events, a.Value.String())
			h.lock.Unlock()
		}
		return true
	})
	return nil
}

func (h *eventRecorder) count(event string) int {
	h.lock.Lock()
	defer h.lock.Unlock()
	n := 0
	for _, e := range h.events {
		if e == event {
			n++
		}
	}
	return n
}

// runOn runs sh -c "exit 5" on the runner, it can be called from any goroutine
func runOn(address string, logger *slog.Logger) error {
	c, err := NewClient(context.Background(), address, logger, nil)
	if err != nil {
		return err
	}
	exited := make(chan ProcState, 1)
	ca := testArgs
	ca.Name, ca.Args = "sh", []string{"sh", "-c", "exit 5"}
	if err := c.StartPty(context.Background(), ca, func(ps ProcState) {
		if !ps.Alive() {
			select {
			case exited <- ps:
			default:
			}
		}
	}); err != nil {
		return err
	}
	term, err := net.Dial("unix", c.PtyPath)
	if err != nil {
		return err
	}
	defer term.Close()
	go func() { _, _ = io.Copy(io.Discard, term) }()
	select {
	case ps := <-exited:
		if !ps.Exited || ps.ExitCode != 5 {
			return fmt.Errorf("the process state is %s", ps.String())
		}
		return nil
	case <-time.After(20 * time.Second):
		return errors.New("the process did not exit")
	}
}

// The first start uploads the binary (once, for parallel starts too), the next ones use it
func TestManagedUpload(t *testing.T) {
	cache, bin := managedSetup(t, localPlatform)
	// a binary that was not used for 40 days is deleted after the upload, a recent one is kept
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	old, recent := filepath.Join(cache, "rtrunner-old"), filepath.Join(cache, "rtrunner-recent")
	for _, f := range []string{old, recent} {
		if err := os.WriteFile(f, []byte("x"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	recorder := &eventRecorder{}
	logger := slog.New(recorder)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- runOn("ssh://tester@fakehost", logger)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if n := recorder.count(EventUpload); n != 1 {
		t.Errorf("%d uploads", n)
	}
	st, err := os.Stat(filepath.Join(cache, "rtrunner-"+bin.hash))
	if err != nil || st.Mode().Perm() != 0700 {
		t.Errorf("the uploaded binary: %v, %v", st, err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the old binary is not deleted: %v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("the recent binary is deleted: %v", err)
	}

	if err := runOn("ssh://tester@fakehost", logger); err != nil {
		t.Error(err)
	}
	if n := recorder.count(EventUpload); n != 1 {
		t.Errorf("%d uploads after the next start", n)
	}
	if tmp, _ := filepath.Glob(filepath.Join(cache, "*.tmp.*")); len(tmp) > 0 {
		t.Errorf("temporary files are left: %v", tmp)
	}
}

// An upload that does not match its hash is not used
func TestUploadIncomplete(t *testing.T) {
	cache, bin := managedSetup(t, localPlatform)
	wrong := *bin
	wrong.hash = strings.Repeat("0", 64)
	a, _ := ParseAddress("ssh://tester@fakehost")
	err := upload(a, &wrong, slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "the upload was not complete?") {
		t.Errorf("the error is %v", err)
	}
	if files, _ := filepath.Glob(filepath.Join(cache, "rtrunner-*")); len(files) > 0 {
		t.Errorf("the files are %v", files)
	}
}

// A host whose platform has no binary
func TestManagedNoPlatform(t *testing.T) {
	managedSetup(t, "plan9-amd64")
	err := runOn("ssh://tester@fakehost", slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "runtree has no rtrunner for the platform of the host: ") {
		t.Errorf("the error is %v", err)
	}
}

func TestManagedNoBinaries(t *testing.T) {
	SetBinaryDir(t.TempDir())
	t.Cleanup(func() { SetBinaryDir("") })
	_, err := NewClient(context.Background(), "ssh://tester@fakehost", slog.New(slog.DiscardHandler), nil)
	if !errors.Is(err, errNoBinaries) {
		t.Errorf("the error is %v", err)
	}
}

func TestCheckOwnSHA256(t *testing.T) {
	content, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if err := CheckOwnSHA256(hex.EncodeToString(sum[:])); err != nil {
		t.Error(err)
	}
	if err := CheckOwnSHA256(strings.Repeat("0", 64)); err == nil {
		t.Error("the wrong hash is accepted")
	}
}

// The scripts are in single quotes for the login shell of the user (it can be csh, where ! is special too)
func TestScripts(t *testing.T) {
	bins := map[string]*runnerBinary{
		"linux-amd64":  {platform: "linux-amd64", hash: strings.Repeat("a", 64)},
		"darwin-arm64": {platform: "darwin-arm64", hash: strings.Repeat("b", 64)},
	}
	for _, script := range []string{startScript(bins), uploadScript(strings.Repeat("a", 64))} {
		if strings.ContainsAny(script, "'!\n") {
			t.Errorf("the script has ', ! or a new line: %s", script)
		}
		if out, err := exec.Command("sh", "-n", "-c", script).CombinedOutput(); err != nil {
			t.Errorf("%s: %v %s", script, err, out)
		}
	}
	if s := startScript(bins); !strings.Contains(s, "Linux/x86_64) p=linux-amd64 h=aaaa") ||
		!strings.Contains(s, "Darwin/arm64) p=darwin-arm64 h=bbbb") || strings.Contains(s, "aarch64") {
		t.Errorf("the start script is %s", s)
	}
}
