package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"github.com/nagylzs/runtree/internal/config"
	"github.com/nagylzs/runtree/internal/runner"
	"github.com/nagylzs/runtree/internal/signal"
	"github.com/nagylzs/runtree/internal/version"
)

func main() {
	var args = config.RtRunnerCLIArgs{
		BaseArgs: config.BaseArgs{
			Debug:   false,
			Verbose: false,
		},
		ListenAddress: config.DefaultListenAddress,
	}
	posArgs, err := flags.ParseArgs(&args, os.Args)
	if err != nil {
		println(err.Error())
		os.Exit(1)
	}

	if args.ShowVersion {
		version.PrintVersion()
		os.Exit(0)
	}

	// Set loglevel
	var programLevel = new(slog.LevelVar)
	if args.Debug {
		programLevel.Set(slog.LevelDebug)
	} else if args.Verbose {
		programLevel.Set(slog.LevelInfo)
	} else {
		programLevel.Set(slog.LevelWarn)
	}

	lw := os.Stderr
	h := slog.New(
		tint.NewHandler(lw, &tint.Options{
			NoColor: !isatty.IsTerminal(lw.Fd()),
			Level:   programLevel,
		}),
	)
	slog.SetDefault(h)

	signal.SetupSignalHandler()

	go func() {
		err = runMain(args, posArgs)
		if err != nil {
			signal.Stop(1)
		}
	}()

	for !signal.IsStopping() {
		time.Sleep(time.Second)
	}

	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func runMain(args config.RtRunnerCLIArgs, posArgs []string) error {
	slog.Info("Listening on " + args.ListenAddress)
	l, err := net.Listen("tcp", args.ListenAddress)
	if err != nil {
		return err
	}
	defer l.Close()

	slog.Info("Waiting for incoming connections")

	var s uint64 = 0
	for !signal.IsStopping() {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		s += 1
		go runConn(conn, s)
	}

	return nil
}

func runConn(conn net.Conn, sessId uint64) {
	logger := slog.With("remote", conn.RemoteAddr(), "session", sessId)
	ctx := context.Background()
	err := runner.RunNewServer(ctx, conn, logger)
	if err != nil {
		logger.Error(err.Error())
	}
}
