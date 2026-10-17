package main

import (
	"errors"
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

	if args.Schema {
		schema, err := rt.JSONSchema()
		if err != nil {
			println(err.Error())
			os.Exit(1)
		}
		_, _ = os.Stdout.Write(schema)
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
	var parseErrs rt.ParseErrors
	var parseErr *rt.ParseError
	if errors.As(err, &parseErrs) {
		// file:line:col: message lines, and their details on separate lines
		_, _ = fmt.Fprintln(os.Stderr, parseErrs.Error())
		if len(parseErrs) > 1 {
			_, _ = fmt.Fprintf(os.Stderr, "%d errors\n", len(parseErrs))
		}
		os.Exit(1)
	} else if errors.As(err, &parseErr) {
		_, _ = fmt.Fprintln(os.Stderr, parseErr.Error())
		os.Exit(1)
	}

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

	sources := rt.NewSources()
	_, filename, err := rt.AddTree(fullFilePath, sources)
	if err != nil {
		return err
	}
	runTree, err := rt.ParseToDom(sources, filename, args.MaxDepth)
	// the warnings are printed before the errors (that are printed by main)
	warnings := sources.Warnings()
	if len(warnings) > 0 {
		_, _ = fmt.Fprintln(os.Stderr, warnings.Error())
	}
	if err != nil {
		return err
	}
	if args.Check {
		_, _ = fmt.Fprintf(os.Stderr, "%s: OK, %d nodes%s\n", posArgs[1], len(runTree.Nodes),
			plural(", %d warning", len(warnings)))
		return nil
	}

	err = gtkui.InitApplication(runTree, warnings)
	if err != nil {
		return fmt.Errorf("could not initialize application: %w", err)
	}

	exitCode = gtkui.RunApplication()
	return nil
}

// plural formats a count with format (e.g. ", %d warning"), with an "s" if count > 1. It is empty when count is 0.
func plural(format string, count int) string {
	if count == 0 {
		return ""
	}
	res := fmt.Sprintf(format, count)
	if count > 1 {
		res += "s"
	}
	return res
}
