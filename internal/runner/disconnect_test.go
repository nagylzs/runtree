package runner

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xtaci/smux"
)

// streamPair returns the two ends of a smux stream over a pipe: the one that the client opened, and the one that the
// server accepted
func streamPair(t *testing.T, client, server *smux.Session) (*smux.Stream, *smux.Stream) {
	t.Helper()
	c, err := client.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}

// running tells if a goroutine has not returned yet (it has not closed done)
func running(done <-chan struct{}) bool {
	select {
	case <-done:
		return false
	case <-time.After(100 * time.Millisecond):
		return true
	}
}

// The output of a process that exits quickly may end (and stop the server) before the server reads the input and the
// commands of the client. The server must still wait for the client to disconnect, and close the pty stream: when it
// closes the connection earlier, the client may lose the exit status and the end of the output, or fail to send its
// disconnect message.
func TestServerWaitsForTheClient(t *testing.T) {
	c1, c2 := net.Pipe()
	clientSession, err := smux.Client(c1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	serverSession, err := smux.Server(c2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientPty, serverPty := streamPair(t, clientSession, serverSession)
	clientCmd, serverCmd := streamPair(t, clientSession, serverSession)

	s := newServer(c2, slog.New(slog.DiscardHandler))
	s.ptyStream, s.cmdStream = serverPty, serverCmd
	s.stop.Store(true) // the end of the output
	commandsDone, inputDone := make(chan struct{}), make(chan struct{})
	s.wg.Add(2)
	go func() {
		s.recvCommands()
		close(commandsDone)
	}()
	go func() {
		s.connectStreams(io.Discard, s.ptyStream, "pty-send")
		close(inputDone)
	}()

	if !running(commandsDone) {
		t.Fatal("the server does not wait for the commands of the client")
	}
	if !running(inputDone) {
		t.Fatal("the server does not wait for the client to close the pty stream")
	}
	ctx := context.Background()
	if err := SendCmdCode(ctx, clientCmd, MsgDisconnect); err != nil {
		t.Fatal(err)
	}
	select {
	case <-commandsDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the server does not stop after the disconnect message")
	}
	if !running(inputDone) {
		t.Fatal("the server does not wait for the client to close the pty stream")
	}
	_ = clientPty.Close()
	select {
	case <-inputDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the server does not stop when the client closes the pty stream")
	}
}

// When the runner closes the connection after the exit status (e.g. an older runner), the client cannot send its
// disconnect message, but the process exited with its exit code: it is not a connection error.
func TestClientKeepsTheExitStatus(t *testing.T) {
	c1, c2, err := socketPair() // buffered: both ends send their version first
	if err != nil {
		t.Fatal(err)
	}
	received, closed := make(chan struct{}), make(chan struct{})
	go func() {
		// the runner: it sends the exit status, and closes the connection when the client has received it
		defer close(closed)
		ctx := context.Background()
		if negotiateVersion(ctx, c2) != nil {
			return
		}
		if code, err := RecvCmdCode(ctx, c2); err != nil || code != MsgStartPty {
			return
		}
		session, err := smux.Server(c2, nil)
		if err != nil {
			return
		}
		defer session.Close()
		if _, err := session.AcceptStream(); err != nil { // the pty stream
			return
		}
		cmdStream, err := session.AcceptStream()
		if err != nil {
			return
		}
		var ca CmdArgs
		if RecvAny(ctx, cmdStream, &ca) != nil || SendCmdCode(ctx, cmdStream, MsgProcState) != nil ||
			SendAny(ctx, cmdStream, &ProcState{Pid: 1, Exited: true, ExitCode: 3}) != nil {
			return
		}
		<-received
	}()

	c, err := newClient(context.Background(), c1, HandshakeTimeout, slog.New(slog.DiscardHandler), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetNoTerminal()
	var once sync.Once
	states := make(chan ProcState, 10)
	ca := CmdArgs{Name: "sh", Args: []string{"sh", "-c", "exit 3"}} // the runner does not start it
	err = c.StartPty(context.Background(), ca, func(ps ProcState) {
		states <- ps
		once.Do(func() {
			// the client sends the disconnect message after this, when the connection is closed
			close(received)
			<-closed
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 { // the exit status, and the client's own end (Close)
		select {
		case ps := <-states:
			if !ps.Exited || ps.ExitCode != 3 || strings.Contains(ps.Error, "pipe") {
				t.Fatalf("state %d is %+v", i, ps)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no state %d", i)
		}
	}
}
