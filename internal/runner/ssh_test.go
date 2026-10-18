//go:build !windows

package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const asRtrunnerEnv = "RUNNER_TEST_AS_RTRUNNER"

// When the test binary is started with asRtrunnerEnv, it works as rtrunner: with --check-sha256 HEX it checks itself,
// otherwise it serves the connection like rtrunner --stdio (it ignores the arguments of ssh when it is the fake ssh).
func TestMain(m *testing.M) {
	if os.Getenv(asRtrunnerEnv) == "1" {
		var err error
		if i := slices.Index(os.Args, "--check-sha256"); i > 0 && i+1 < len(os.Args) {
			err = CheckOwnSHA256(os.Args[i+1])
		} else {
			err = ServeStdio(context.Background(), slog.New(slog.DiscardHandler))
		}
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	// the tests do not share ssh connections in the runtime directory of the user, see TestSSHShare
	SetSSHShare(false)
	os.Exit(m.Run())
}

// setSSH sets the ssh command for the test
func setSSH(t *testing.T, command ...string) {
	saved := sshCommand
	sshCommand = command
	t.Cleanup(func() { sshCommand = saved })
}

func TestParseSSHAddress(t *testing.T) {
	for s, want := range map[string]string{
		"ssh://server01":                     "host=server01 user= port= path=",
		"ssh://root@server01":                "host=server01 user=root port= path=",
		"ssh://root@server01:2222":           "host=server01 user=root port=2222 path=",
		"ssh://root@server01/":               "host=server01 user=root port= path=",
		"ssh://server01/usr/local/bin/rtr":   "host=server01 user= port= path=/usr/local/bin/rtr",
		"ssh://server01/~/bin/rtrunner":      "host=server01 user= port= path=~/bin/rtrunner",
		"ssh://[::1]:22":                     "host=::1 user= port=22 path=",
		"ssh://root:secret@server01":         "error: invalid runner \"ssh://root:secret@server01\": passwords",
		"ssh://-oProxyCommand=x":             "error: invalid runner \"ssh://-oProxyCommand=x\": the user and",
		"ssh://-l@server01":                  "error: invalid runner \"ssh://-l@server01\": the user and the host cannot",
		"ssh://server01:":                    "error: invalid runner \"ssh://server01:\": the port is missing",
		"ssh://server01:x":                   "error: invalid runner",
		"ssh://":                             "error: invalid runner",
		"ssh://server01?x=1":                 "error: invalid runner",
		"ssh:server01":                       "error: invalid runner",
		"ssh://root@server01:2222/~/x/rtr":   "host=server01 user=root port=2222 path=~/x/rtr",
		"ssh://server01/opt/my runner/rtr":   "host=server01 user= port= path=/opt/my runner/rtr",
		"ssh://server01/opt/my%20dir/rtr":    "host=server01 user= port= path=/opt/my dir/rtr",
		"ssh://deploy@server01/opt/rtrunner": "host=server01 user=deploy port= path=/opt/rtrunner",
	} {
		a, err := ParseAddress(s)
		got := fmt.Sprintf("host=%s user=%s port=%s path=%s", a.Host, a.User, a.Port, a.Path)
		if err != nil {
			got = "error: " + err.Error()
		} else if a.Network != NetworkSSH {
			got = "network " + a.Network
		}
		if !strings.HasPrefix(got, want) {
			t.Errorf("%q: got %q, want %q", s, got, want)
		}
		if err == nil {
			// the address can be given again
			if b, err := ParseAddress(a.String()); err != nil || b != a {
				t.Errorf("%q: String() is %q, parsed: %+v, %v", s, a.String(), b, err)
			}
		}
	}
}

func TestSSHArgs(t *testing.T) {
	for s, want := range map[string]string{
		"ssh://server01/usr/bin/rtrunner":     "-T -o BatchMode=yes -- server01 /usr/bin/rtrunner --stdio",
		"ssh://root@server01:2222/x/rtrunner": "-T -o BatchMode=yes -p 2222 -- root@server01 /x/rtrunner --stdio",
		"ssh://server01/~/bin/rtrunner":       "-T -o BatchMode=yes -- server01 ~/bin/rtrunner --stdio",
		"ssh://server01/opt/it's/rtrun":       `-T -o BatchMode=yes -- server01 '/opt/it'\''s/rtrun' --stdio`,
		"ssh://server01/opt/a b/rtrunner":     "-T -o BatchMode=yes -- server01 '/opt/a b/rtrunner' --stdio",
	} {
		a, err := ParseAddress(s)
		if err != nil {
			t.Fatal(err)
		}
		args := append(sshOptions(a), "--", sshDest(a), remoteCommand(a))
		if got := strings.Join(args, " "); got != want {
			t.Errorf("%s: got %s, want %s", s, got, want)
		}
	}
}

// A process on an ssh runner: ssh is the test binary, that serves the connection like rtrunner --stdio
func TestSSHRunner(t *testing.T) {
	t.Setenv(asRtrunnerEnv, "1")
	setSSH(t, os.Args[0])
	logger := slog.New(slog.DiscardHandler)
	c, err := NewClient(context.Background(), "ssh://tester@fakehost:2222/opt/rtrunner", logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	exited := make(chan ProcState, 1)
	ca := testArgs
	ca.Name, ca.Args = "sh", []string{"sh", "-c", "echo hello; exit 4"}
	if err := c.StartPty(context.Background(), ca, func(ps ProcState) {
		if !ps.Alive() {
			select {
			case exited <- ps:
			default:
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	output := attach(t, c)
	if ps := waitExit(t, exited); !ps.Exited || ps.ExitCode != 4 {
		t.Errorf("the process state is %s", ps.String())
	}
	if out := waitOutput(t, output); out != "hello\r\n" {
		t.Errorf("the output is %q", out)
	}
}

// When ssh fails, the error has its messages
func TestSSHErrors(t *testing.T) {
	for _, tc := range []struct {
		script string
		want   []string
	}{
		{`echo "Permission denied (publickey)." >&2; exit 255`,
			[]string{"ssh tester@fakehost: Permission denied (publickey)."}},
		{`echo "bash: line 1: rtrunner: command not found" >&2; exit 127`,
			[]string{"ssh tester@fakehost: bash: line 1: rtrunner: command not found", "is rtrunner installed"}},
		// the arguments of ssh
		{`echo "$@" >&2; exit 255`,
			[]string{"ssh tester@fakehost: -T -o BatchMode=yes -p 2222 -- tester@fakehost /opt/rtrunner --stdio"}},
		{`exit 255`, []string{"ssh tester@fakehost: "}}, // the error of the handshake
	} {
		setSSH(t, "sh", "-c", tc.script, "ssh")
		_, err := NewClient(context.Background(), "ssh://tester@fakehost:2222/opt/rtrunner",
			slog.New(slog.DiscardHandler), nil)
		if err == nil {
			t.Fatalf("%s: no error", tc.script)
		}
		for _, want := range tc.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the error is %q, want %q in it", tc.script, err, want)
			}
		}
	}
}

// the arguments of the fake ssh are not mixed up between the tests
func TestSSHCommandNotChanged(t *testing.T) {
	if !slices.Equal(sshCommand, []string{"ssh"}) {
		t.Errorf("the ssh command is %v", sshCommand)
	}
}

// The options of the shared ssh connections, and their directory
func TestSSHShare(t *testing.T) {
	a, _ := ParseAddress("ssh://root@server01:2222")
	t.Cleanup(func() { SetSSHShare(false) })
	run, cache := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	for _, tc := range []struct {
		runtimeDir, dir string
	}{
		{run, filepath.Join(run, "runtree")},
		{"", filepath.Join(cache, "runtree", "ssh")},      // e.g. macOS
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
