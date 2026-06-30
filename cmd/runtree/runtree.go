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
	"gopkg.in/yaml.v2"
)

const MaxYamlFileSize = 1024 * 1024

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
	inputFile := posArgs[1]
	allTrees := make(map[string]map[string]interface{})
	runTree, err := addTree(inputFile, allTrees, args.MaxDepth)
	if err != nil {
		return err
	}

	err = gtkui.InitApplication(runTree)
	if err != nil {
		return fmt.Errorf("could not initialize application: %w", err)
	}

	exitCode = gtkui.RunApplication()
	return nil
}

func addTree(inputFile string, allTrees map[string]map[string]interface{}, maxDepth uint) (*rt.Tree, error) {
	fi, err := os.Stat(inputFile)
	if err != nil {
		return nil, fmt.Errorf("could not stat input file %v: %w", inputFile, err)
	}
	if fi.Size() > MaxYamlFileSize {
		return nil, fmt.Errorf("input file %v bigger than %v bytes", inputFile, MaxYamlFileSize)
	}
	filename := filepath.Base(inputFile)
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return nil, fmt.Errorf("could not read input file %v: %w", inputFile, err)
	}
	var rawTrees map[string]interface{}
	err = yaml.Unmarshal(data, &rawTrees)
	if err != nil {
		return nil, fmt.Errorf("could not parse input file %v: %w", inputFile, err)
	}
	allTrees[filename] = rawTrees
	runTree, err := rt.ParseToDom(allTrees, filename, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("could not parse input file %v: %w", inputFile, err)
	}
	return runTree, nil
}
