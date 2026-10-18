package runner

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/nagylzs/runtree/internal/logging"
)

// The local runner is embedded in runtree: each client gets one end of a socket pair, and a goroutine serves the other
// end, as rtrunner serves a TCP connection.

// StopLocalTimeout is enough for the grace periods of the termination, and for the clients to get the exit statuses
// (see Client.recvProcState).
const StopLocalTimeout = TerminateGraceInt + TerminateGraceTerm + 5*time.Second

var errLocalStopped = errors.New("the local runner is stopped, runtree is exiting")

type localRunner struct {
	lock     sync.Mutex
	stopped  bool
	sessions uint64
	servers  map[*Server]struct{} // the servers that have not returned yet
	wg       sync.WaitGroup       // the servers that have not returned yet
	handler  slog.Handler         // of the servers' logs, nil for the default logger
}

var local = newLocalRunner()

func newLocalRunner() *localRunner {
	return &localRunner{servers: make(map[*Server]struct{})}
}

// dial starts a server for a new connection, and returns the client's end of the connection
func (l *localRunner) dial() (net.Conn, error) {
	l.lock.Lock()
	defer l.lock.Unlock()
	if l.stopped {
		return nil, errLocalStopped
	}
	clientConn, serverConn, err := socketPair()
	if err != nil {
		return nil, err
	}
	l.sessions++
	// "started" and "exited" are debug messages here: the node's events tell them already
	h := l.handler
	if h == nil {
		h = slog.Default().Handler()
	}
	logger := slog.New(logging.InfoAsDebug(h)).With("runner", LocalAddress, "session", l.sessions)
	srv := newServer(serverConn, logger)
	l.servers[srv] = struct{}{}
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		if err := srv.Run(context.Background()); err != nil {
			logger.Error(err.Error())
		}
		_ = serverConn.Close()
		l.lock.Lock()
		delete(l.servers, srv)
		l.lock.Unlock()
	}()
	return clientConn, nil
}

// running returns the number of servers that have not returned yet
func (l *localRunner) running() int {
	l.lock.Lock()
	defer l.lock.Unlock()
	return len(l.servers)
}

// processes returns the number of processes that are running
func (l *localRunner) processes() int {
	l.lock.Lock()
	defer l.lock.Unlock()
	n := 0
	for srv := range l.servers {
		if srv.processRunning() {
			n++
		}
	}
	return n
}

// stop terminates the processes, and waits until the servers return, at most for timeout. It tells if they all
// returned. No process can be started afterwards.
func (l *localRunner) stop(timeout time.Duration) bool {
	l.lock.Lock()
	l.stopped = true
	servers := make([]*Server, 0, len(l.servers))
	for srv := range l.servers {
		servers = append(servers, srv)
	}
	l.lock.Unlock()
	for _, srv := range servers {
		srv.Terminate()
	}
	done := make(chan struct{})
	go func() {
		l.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// LocalProcesses returns the number of processes of the local runner that are running
func LocalProcesses() int {
	return local.processes()
}

// StopLocal terminates the processes of the local runner as rtrunner does when its client is gone (SIGINT, then
// SIGTERM and SIGKILL after the grace periods), and waits until their clients got their exit statuses, at most for
// timeout. It tells if they all finished. No local process can be started afterwards.
func StopLocal(timeout time.Duration) bool {
	return local.stop(timeout)
}
