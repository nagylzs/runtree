package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const asRtrunnerEnv = "RUNNER_TEST_AS_RTRUNNER"

// failingSSHEnv is "CODE MESSAGE": the test binary is an ssh that writes MESSAGE to its standard error, ARGS replaced
// with its arguments, and exits with CODE
const failingSSHEnv = "RUNNER_TEST_FAILING_SSH"

// When the test binary is started with asRtrunnerEnv, it works as rtrunner: with --check-sha256 HEX it checks itself,
// otherwise it serves the connection like rtrunner --stdio (it ignores the arguments of ssh when it is the fake ssh).
// With failingSSHEnv, it is an ssh that fails.
func TestMain(m *testing.M) {
	if v := os.Getenv(failingSSHEnv); v != "" {
		code, msg, _ := strings.Cut(v, " ")
		if msg = strings.ReplaceAll(msg, "ARGS", strings.Join(os.Args[1:], " ")); msg != "" {
			_, _ = fmt.Fprintln(os.Stderr, msg)
		}
		exitCode, _ := strconv.Atoi(code)
		os.Exit(exitCode)
	}
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
		"ssh://nagyl@htpc/C:/rt/rtrunner":    "host=htpc user=nagyl port= path=C:/rt/rtrunner",
		"ssh://htpc/c:/Program Files/rt.exe": "host=htpc user= port= path=c:/Program Files/rt.exe",
		"ssh://server01/C:x":                 "host=server01 user= port= path=/C:x",
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
		"ssh://server01/opt/it's/rtrun":       `-T -o BatchMode=yes -- server01 "/opt/it's/rtrun" --stdio`,
		"ssh://server01/opt/a b/rtrunner":     `-T -o BatchMode=yes -- server01 "/opt/a b/rtrunner" --stdio`,
		"ssh://server01/opt/it's $x/rtr":      `-T -o BatchMode=yes -- server01 '/opt/it'\''s $x/rtr' --stdio`,
		"ssh://server01/opt/50%25/rtrunner":   "-T -o BatchMode=yes -- server01 '/opt/50%/rtrunner' --stdio",
		// cmd.exe on Windows
		"ssh://htpc/C:/rt/rtrunner.exe":      "-T -o BatchMode=yes -- htpc C:/rt/rtrunner.exe --stdio",
		"ssh://htpc/C:/Program Files/rt.exe": `-T -o BatchMode=yes -- htpc "C:/Program Files/rt.exe" --stdio`,
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
	output := newClosedBuffer()
	c.SetOutput(output)
	c.SetNoTerminal()
	exited := make(chan ProcState, 1)
	ca := cmdArgs("sh", "-c", "echo hello; exit 4")
	if runtime.GOOS == "windows" {
		ca = cmdArgs("cmd", "/c", "echo hello & exit /b 4")
	}
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
	if ps := waitExit(t, exited); !ps.Exited || ps.ExitCode != 4 {
		t.Errorf("the process state is %s", ps.String())
	}
	if out := output.wait(t); !strings.Contains(out, "hello\r\n") {
		t.Errorf("the output is %q", out)
	}
}

// When the connection is closed, ssh exits (rtrunner gets the end of its input)
func TestSSHEndsWithTheConnection(t *testing.T) {
	t.Setenv(asRtrunnerEnv, "1")
	setSSH(t, os.Args[0])
	a, _ := ParseAddress("ssh://tester@fakehost/opt/rtrunner")
	conn, err := dialSSH(a, remoteCommand(a), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := negotiateVersion(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	select {
	case <-conn.exited:
	case <-time.After(10 * time.Second):
		t.Fatal("ssh did not exit")
	}
}

// When ssh fails, the error has its messages
func TestSSHErrors(t *testing.T) {
	setSSH(t, os.Args[0])
	for _, tc := range []struct {
		ssh  string // see failingSSHEnv
		want []string
	}{
		{"255 Permission denied (publickey).", []string{"ssh tester@fakehost: Permission denied (publickey)."}},
		{"127 bash: line 1: rtrunner: command not found",
			[]string{"ssh tester@fakehost: bash: line 1: rtrunner: command not found", "is rtrunner installed"}},
		// the arguments of ssh
		{"255 ARGS", []string{"ssh tester@fakehost: -T -o BatchMode=yes -p 2222 -- tester@fakehost /opt/rtrunner --stdio"}},
		{"255", []string{"ssh tester@fakehost: "}}, // the error of the handshake
	} {
		t.Setenv(failingSSHEnv, tc.ssh)
		began := time.Now()
		_, err := NewClient(context.Background(), "ssh://tester@fakehost:2222/opt/rtrunner",
			slog.New(slog.DiscardHandler), nil)
		if err == nil {
			t.Fatalf("%s: no error", tc.ssh)
		}
		for _, want := range tc.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the error is %q, want %q in it", tc.ssh, err, want)
			}
		}
		// the end of ssh is the end of the connection: the handshake does not wait for its timeout
		if elapsed := time.Since(began); elapsed > 10*time.Second {
			t.Errorf("%s: the error came after %v", tc.ssh, elapsed)
		}
	}
}

// the arguments of the fake ssh are not mixed up between the tests
func TestSSHCommandNotChanged(t *testing.T) {
	if !slices.Equal(sshCommand, []string{"ssh"}) {
		t.Errorf("the ssh command is %v", sshCommand)
	}
}
