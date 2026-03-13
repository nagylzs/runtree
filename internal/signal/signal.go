package signal

import (
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
)

var exitCode int = 0
var sigs = make(chan os.Signal, 1)
var stopRequested uint32 = 0

func Stop(exitCode uint32) {
	atomic.StoreUint32(&exitCode, exitCode)
	atomic.StoreUint32(&stopRequested, 1)
}

func IsStopping() bool {
	return atomic.LoadUint32(&stopRequested) > 0
}

func Exit() {
	os.Exit(exitCode)
}

func SetupSignalHandler() {
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go exitOnSignal()
}

func exitOnSignal() {
	sig := <-sigs
	slog.Warn("Exit on signal.", "signal", sig.String())
	Stop(1)
}
