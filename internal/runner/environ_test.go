//go:build !windows

package runner

import (
	"testing"
)

// The processes of the local runner get the environment of runtree, but not the hidden variables
func TestHideEnv(t *testing.T) {
	t.Setenv("RUNTREE_TEST_HIDDEN", "hidden")
	t.Setenv("RUNTREE_TEST_SHOWN", "shown")
	HideEnv("RUNTREE_TEST_HIDDEN")
	l := newTestLocalRunner()
	_, output, exited := startLocal(t, l, "sh", "-c", `echo "[$RUNTREE_TEST_HIDDEN] [$RUNTREE_TEST_SHOWN]"`)
	waitExit(t, exited)
	if out := waitOutput(t, output); out != "[] [shown]\r\n" {
		t.Errorf("the output is %q", out)
	}
	waitServers(t, l)
}
