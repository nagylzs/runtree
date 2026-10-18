package runner

import (
	"context"
	"errors"
	"log/slog"
	"os"
	ossignal "os/signal"
	"syscall"
)

// ServeStdio serves one connection on the standard input and output (rtrunner --stdio, started by the ssh runner of
// runtree). Nothing else may write to the standard output.
func ServeStdio(ctx context.Context, logger *slog.Logger) error {
	// When the client is gone, a write to the standard output is an error (and the process is terminated as usual),
	// not a SIGPIPE that kills the runner. (A notified signal is not inherited by the processes, an ignored one is.)
	ossignal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	return RunNewServer(ctx, stdioConn{}, logger)
}

type stdioConn struct{}

func (stdioConn) Read(p []byte) (int, error) {
	return os.Stdin.Read(p)
}

func (stdioConn) Write(p []byte) (int, error) {
	return os.Stdout.Write(p)
}

func (stdioConn) Close() error {
	return errors.Join(os.Stdin.Close(), os.Stdout.Close())
}
