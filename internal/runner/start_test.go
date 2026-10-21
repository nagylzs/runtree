package runner

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xtaci/smux"
)

// A command that cannot be started, because its program or its working directory is missing: the client gets the
// error of the start as the final state of the process (not a lost connection), and the server returns when the client
// has disconnected.
func TestStartError(t *testing.T) {
	exe, err := os.Executable() // a program that runs on every system: this test
	if err != nil {
		t.Fatal(err)
	}
	inDir := func(dir string) CmdArgs {
		ca := cmdArgs(exe, "-test.run=^$")
		ca.Cwd = dir
		return ca
	}
	missingDir := filepath.Join(t.TempDir(), "missing")
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l := newTestLocalRunner()
	for _, tc := range []struct {
		name string
		ca   CmdArgs
		want string
	}{
		{"missing program", cmdArgs("runtree-no-such-program", "arg"), `"runtree-no-such-program"`},
		{"missing cwd", inDir(missingDir), "chdir " + missingDir + ": "},
		{"cwd is a file", inDir(file), "chdir " + file + ": not a directory"},
	} {
		conn, err := l.dial()
		if err != nil {
			t.Fatal(err)
		}
		out := newClosedBuffer()
		_, exited := startNoTerminal(t, conn, tc.ca, out)
		ps := waitExit(t, exited)
		if ps.Exited || ps.Pid != 0 || !strings.Contains(ps.Error, tc.want) {
			t.Errorf("%s: the process state is %s, want the error of the start with %s", tc.name, ps.String(), tc.want)
		}
		out.wait(t)
	}
	waitServers(t, l)
}

// After the error of the start, the server waits for the client to disconnect, as after an exit: when it closes the
// connection first, the client may lose the error (see TestServerWaitsForTheClient).
func TestServerWaitsAfterAStartError(t *testing.T) {
	clientConn, serverConn, err := socketPair()
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	srv := newServer(serverConn, slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() {
		if err := srv.Run(context.Background()); err != nil {
			t.Errorf("the server returned an error: %v", err)
		}
		close(done)
	}()

	// the client, without Client: it does not disconnect by itself
	ctx := context.Background()
	if err := negotiateVersion(ctx, clientConn); err != nil {
		t.Fatal(err)
	}
	if err := SendCmdCode(ctx, clientConn, MsgStartPty); err != nil {
		t.Fatal(err)
	}
	session, err := smux.Client(clientConn, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.OpenStream(); err != nil { // the pty stream
		t.Fatal(err)
	}
	cmdStream, err := session.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if err := SendAny(ctx, cmdStream, cmdArgs("runtree-no-such-program")); err != nil {
		t.Fatal(err)
	}
	var ps ProcState
	if code, err := RecvCmdCode(ctx, cmdStream); err != nil || code != MsgProcState {
		t.Fatalf("the server sent %v, %v instead of the process state", code, err)
	}
	if err := RecvAny(ctx, cmdStream, &ps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ps.Error, "runtree-no-such-program") {
		t.Errorf("the process state is %s", ps.String())
	}
	// a resize before the client knew that the process is not started
	if err := SendCmdCode(ctx, cmdStream, MsgResize); err != nil {
		t.Fatal(err)
	}
	if err := SendAny(ctx, cmdStream, &WinSize{Rows: 30, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	if !running(done) {
		t.Fatal("the server returned before the client disconnected")
	}
	if err := SendCmdCode(ctx, cmdStream, MsgDisconnect); err != nil {
		t.Fatal(err)
	}
	waitDone(t, done, "the server did not return after the client disconnected")
}
