// runtree-headless runs a tree without the GUI, unattended (e.g. in CI, or from cron), see internal/headless and
// docs/headless.md
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jessevdk/go-flags"
	"github.com/nagylzs/runtree/internal/config"
	"github.com/nagylzs/runtree/internal/headless"
	"github.com/nagylzs/runtree/internal/logging"
	_ "github.com/nagylzs/runtree/internal/rtrunners" // the rtrunner binaries, with -tags rtrunners
	"github.com/nagylzs/runtree/internal/runner"
	"github.com/nagylzs/runtree/internal/version"
)

func main() {
	args := config.HeadlessCLIArgs{RunArgs: config.RunArgs{MaxDepth: config.DefaultMaxDepth}}
	parser := flags.NewParser(&args, flags.Default)
	parser.Usage = "[OPTIONS] TREE_FILE"
	posArgs, err := parser.ParseArgs(os.Args)
	if err != nil {
		// go-flags has printed the help or the error already
		if flags.WroteHelp(err) {
			os.Exit(headless.ExitSuccess)
		}
		os.Exit(headless.ExitError)
	}
	if args.ShowVersion {
		version.PrintVersion("runtree-headless")
		os.Exit(headless.ExitSuccess)
	}
	if len(posArgs) != 2 {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: runtree-headless [OPTIONS] TREE_FILE (runtree-headless --help lists the options)\n")
		os.Exit(headless.ExitError)
	}
	if _, err := runner.ParseAddress(args.Runner); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "--runner: "+err.Error())
		os.Exit(headless.ExitError)
	}
	logging.Setup(args.Verbose, args.Debug, args.LogFormat)

	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	os.Exit(headless.Run(args, posArgs[1], os.Stdout, os.Stderr, sigs))
}
