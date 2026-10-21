//go:build windows

package headless

import (
	"strings"
	"testing"
)

// On Windows, the processes run in ConPTY pseudo consoles, with cmd instead of sh. Their output has terminal control
// sequences around the lines, so the tests look for the text in it.

func TestRunOnWindows(t *testing.T) {
	t.Parallel()
	r := run(t, `
tree:
  title: all
  seq:
    - {title: hello, args: [cmd, /c, "echo hi"]}
    - {title: allowed to fail, on_error: success, args: [cmd, /c, "exit /b 1"]}
`, nil)
	r.check(t, ExitSuccess, []string{"hello           | ", "hi"}, []string{"hello: exited with code 0 after ",
		"hello: Running → Success\n", "all: Running → Success\n", "The run ended after ", ": Success.\n"})

	r = run(t, `
tree:
  seq:
    - {title: build, on_error: paused, args: [cmd, /c, "echo compiling & exit /b 3"]}
    - {title: deploy, args: [cmd, /c, "echo deploying"]}
`, nil)
	r.check(t, ExitFailed, []string{"build  | ", "compiling"}, []string{
		"WARN build: Running → Failed (on_error is paused, but nobody can decide: the tree runs unattended)\n",
		": Failed.\nFailed:\n  build: exit code 3\n",
	})
	if strings.Contains(r.stderr, "deploy: starting") {
		t.Errorf("deploy is started:\n%s", r.stderr)
	}
}

// A command that cannot be started fails with the error of the start
func TestRunStartErrorOnWindows(t *testing.T) {
	t.Parallel()
	r := run(t, "tree:\n  title: missing\n  args: [runtree-no-such-program]\n", nil)
	r.check(t, ExitFailed, nil, []string{
		"Failed:\n  missing: exec: \"runtree-no-such-program\": executable file not found in %PATH%\n"})
}
