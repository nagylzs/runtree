package headless

import (
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nagylzs/runtree/internal/app"
	"github.com/nagylzs/runtree/internal/config"
	"github.com/nagylzs/runtree/internal/lockfile"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/runner"
)

func writeTree(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tree.rt.yaml")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadTree(t *testing.T, source string) *rt.Tree {
	t.Helper()
	tree, _, _, err := app.LoadTree(writeTree(t, source), "", config.DefaultMaxDepth)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// startRunner starts a TCP runner for the test, and returns its address. The tests do not use the local runner:
// a run stops it when it ends, then it cannot start processes anymore.
func startRunner(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() { _ = runner.RunNewServer(context.Background(), conn, slog.New(slog.DiscardHandler)) }()
		}
	}()
	return l.Addr().String()
}

type result struct {
	code           int
	stdout, stderr string
}

// run runs the tree with runtree-headless, opts can change the options
func run(t *testing.T, source string, sigs chan os.Signal, opts ...func(*config.HeadlessCLIArgs)) result {
	t.Helper()
	args := config.HeadlessCLIArgs{RunArgs: config.RunArgs{MaxDepth: config.DefaultMaxDepth, Runner: startRunner(t),
		NoSSHShare: true}}
	for _, o := range opts {
		o(&args)
	}
	if sigs == nil {
		sigs = make(chan os.Signal)
	}
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	done := make(chan int, 1)
	go func() { done <- Run(args, writeTree(t, source), stdout, stderr, sigs) }()
	select {
	case code := <-done:
		return result{code, stdout.String(), stderr.String()}
	case <-time.After(30 * time.Second):
		t.Fatalf("the run did not end, stderr:\n%s", stderr.String())
	}
	return result{}
}

func (r result) check(t *testing.T, code int, stdout []string, stderr []string) {
	t.Helper()
	if r.code != code {
		t.Errorf("the exit code is %d, want %d", r.code, code)
	}
	for _, s := range stdout {
		if !strings.Contains(r.stdout, s) {
			t.Errorf("%q is not in the output:\n%s", s, r.stdout)
		}
	}
	for _, s := range stderr {
		if !strings.Contains(r.stderr, s) {
			t.Errorf("%q is not in stderr:\n%s", s, r.stderr)
		}
	}
}

func TestRunSuccess(t *testing.T) {
	t.Parallel()
	r := run(t, `
tree:
  title: all
  seq:
    - {title: hello, args: [sh, -c, "echo hi; echo there"]}
    - {title: allowed to fail, on_error: success, args: [sh, -c, "exit 1"]}
`, nil)
	r.check(t, ExitSuccess, []string{"hello           | hi\nhello           | there\n"},
		[]string{"hello: Running → Success\n", "all: Running → Success\n", "The run ended after ", ": Success.\n"})
	if strings.Contains(r.stderr, "Failed:") {
		t.Errorf("there are failed nodes:\n%s", r.stderr)
	}
}

// on_error: paused fails: nobody decides
func TestRunFailed(t *testing.T) {
	t.Parallel()
	r := run(t, `
tree:
  seq:
    - {title: build, on_error: paused, args: [sh, -c, "echo compiling; exit 3"]}
    - {title: deploy, args: ["true"]}
`, nil)
	r.check(t, ExitFailed, []string{"build  | compiling\n"}, []string{
		"WARN build: Running → Failed (on_error is paused, but nobody can decide: the tree runs unattended)\n",
		": Failed.\nFailed:\n  build: exit code 3\n",
	})
	if strings.Contains(r.stderr, "deploy: starting") {
		t.Errorf("deploy is started:\n%s", r.stderr)
	}
}

// The run stops when nothing runs and nothing can be started: the frozen hosts of a rolling upgrade are listed
func TestRunStopped(t *testing.T) {
	t.Parallel()
	r := run(t, `
tree:
  title: upgrade
  max_proc: 1
  children_on_error: [paused, freeze]
  for_vars:
    host: [web01, web02, web03]
  par:
    - title: "{host} upgrade"
      children_on_error: failed
      seq:
        - {title: "{host} step", args: [sh, -c, "test {host} != web01"]}
`, nil)
	r.check(t, ExitStopped, nil, []string{
		"The run stopped after ",
		": nothing runs, and nothing can be started.\nWaiting for:\n  web02 upgrade: frozen (by on_error)\n" +
			"  web03 upgrade: frozen (by on_error)\nFailed:\n  web01 step: exit code 1\n",
	})
}

// A signal terminates the processes
func TestRunInterrupted(t *testing.T) {
	t.Parallel()
	sigs := make(chan os.Signal, 1)
	go func() {
		time.Sleep(time.Second)
		sigs <- os.Interrupt
	}()
	r := run(t, `
tree:
  par:
    - {title: long, args: [sleep, "100"]}
`, sigs)
	r.check(t, 130, nil, []string{"SIGINT: terminating the processes", "The run was interrupted after ",
		"(SIGINT).\nFailed:\n  long: "})
}

// --check only loads the tree, the errors of the tree and a locked lock file have their exit codes
func TestRunErrors(t *testing.T) {
	t.Parallel()
	check := func(a *config.HeadlessCLIArgs) { a.Check = true }
	run(t, "tree:\n  args: [\"true\"]\n", nil, check).check(t, ExitSuccess, nil, nil)
	run(t, "tree:\n  args: [\"true\"]\n  unknown: 1\n", nil).check(t, ExitError, nil, nil)

	lockPath := filepath.Join(t.TempDir(), "tree.lock")
	lock, err := lockfile.Acquire(lockPath, "the test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	r := run(t, "tree:\n  args: [\"true\"]\n", nil, func(a *config.HeadlessCLIArgs) { a.Lock = lockPath })
	r.check(t, app.ExitLocked, nil, nil)
	if r.stdout != "" {
		t.Errorf("the tree is run:\n%s", r.stdout)
	}
}
