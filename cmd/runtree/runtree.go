package main

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"
	_ "net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"

	"github.com/jessevdk/go-flags"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"github.com/nagylzs/runtree/internal/config"
	"github.com/nagylzs/runtree/internal/gtkui"
	"github.com/nagylzs/runtree/internal/rt"
	"github.com/nagylzs/runtree/internal/version"
)

var exitCode int = 0

func main() {
	var args = config.CLIArgs{BaseArgs: config.BaseArgs{Verbose: false, Debug: false}, MaxDepth: config.DefaultMaxDepth}
	posArgs, err := flags.ParseArgs(&args, os.Args)
	if err != nil {
		println(err.Error())
		os.Exit(1)
	}

	if args.ShowVersion {
		version.PrintVersion()
		println("GTK 4 app id: " + gtkui.ApplicationId)
		os.Exit(0)
	}

	if len(posArgs) < 2 {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: runtree [options] <runtree_yaml_file>\n")
		os.Exit(1)
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

	if args.CPUProfile != "" {
		f, err := os.Create(args.CPUProfile)
		if err != nil {
			slog.Error("could not create CPU profile", "error", err.Error())
			os.Exit(-1)
		}
		defer f.Close() // error handling omitted for example
		if err := pprof.StartCPUProfile(f); err != nil {
			slog.Error("could not start CPU profile", "error", err.Error())
			os.Exit(-1)
		}
		defer pprof.StopCPUProfile()
	}

	if args.NetProfile > 0 {
		go func() {
			log.Println(http.ListenAndServe(fmt.Sprintf("localhost:%d", args.NetProfile), nil))
		}()
		slog.Warn(fmt.Sprintf("Open http://localhost:%d/debug/pprof/ for live profiling", args.NetProfile))
	}

	err = runMain(args, posArgs)

	if args.MemProfile != "" {
		f, err := os.Create(args.MemProfile)
		if err != nil {
			slog.Error("could not create memory profile", "error", err.Error())
		}
		defer f.Close() // error handling omitted for example
		runtime.GC()    // get up-to-date statistics
		// Lookup("allocs") creates a profile similar to go test -memprofile.
		// Alternatively, use Lookup("heap") for a profile that has inuse_space as the default index.
		if err := pprof.Lookup("allocs").WriteTo(f, 0); err != nil {
			slog.Error("could not write memory profile", "error", err.Error())
		}
	}

	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	os.Exit(exitCode)
}

func runMain(args config.CLIArgs, posArgs []string) error {
	fullFilePath, err := filepath.Abs(posArgs[1])
	if err != nil {
		return fmt.Errorf("could open input file %v: %w", fullFilePath, err)
	}

	allTrees := make(map[string]map[string]interface{})
	_, filename, err := rt.AddTree(fullFilePath, allTrees)
	if err != nil {
		return err
	}
	runTree, err := rt.ParseToDom(allTrees, filename, args.MaxDepth)
	if err != nil {
		return fmt.Errorf("could not parse input file %v: %w", fullFilePath, err)
	}

	err = gtkui.InitApplication(runTree)
	if err != nil {
		return fmt.Errorf("could not initialize application: %w", err)
	}

	exitCode = gtkui.RunApplication()
	return nil
}
