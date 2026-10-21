package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"

	"github.com/jessevdk/go-flags"
	"github.com/nagylzs/runtree/internal/app"
	"github.com/nagylzs/runtree/internal/config"
	"github.com/nagylzs/runtree/internal/gtkui"
	"github.com/nagylzs/runtree/internal/lockfile"
	"github.com/nagylzs/runtree/internal/logdb"
	"github.com/nagylzs/runtree/internal/logging"
	"github.com/nagylzs/runtree/internal/rt"
	_ "github.com/nagylzs/runtree/internal/rtrunners" // the rtrunner binaries, with -tags rtrunners
	"github.com/nagylzs/runtree/internal/runner"
	"github.com/nagylzs/runtree/internal/version"
)

var exitCode int = 0

func main() {
	var args = config.CLIArgs{BaseArgs: config.BaseArgs{Verbose: false, Debug: false},
		RunArgs: config.RunArgs{MaxDepth: config.DefaultMaxDepth}}
	parser := flags.NewParser(&args, flags.Default)
	parser.Usage = "[OPTIONS] TREE_FILE"
	posArgs, err := parser.ParseArgs(os.Args)
	if err != nil {
		// go-flags has printed the help or the error already
		if flags.WroteHelp(err) {
			os.Exit(0)
		}
		os.Exit(1)
	}

	if args.ShowVersion {
		version.PrintVersion()
		fmt.Println("GTK 4 app id: " + gtkui.ApplicationId)
		os.Exit(0)
	}

	if args.Schema {
		schema, err := rt.JSONSchema()
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		_, _ = os.Stdout.Write(schema)
		os.Exit(0)
	}

	if args.ShowRuns || args.ShowEvents || args.ShowOutput != "" {
		if err := showLog(args); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}

	if len(posArgs) < 2 {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: runtree [OPTIONS] TREE_FILE (runtree --help lists the options)\n")
		os.Exit(1)
	}
	if _, err := runner.ParseAddress(args.Runner); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "--runner: "+err.Error())
		os.Exit(1)
	}

	logging.Setup(args.Verbose, args.Debug, args.LogFormat)

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
			err := http.ListenAndServe(fmt.Sprintf("localhost:%d", args.NetProfile), nil)
			slog.Error("the profile http server stopped", "error", err)
		}()
		slog.Warn(fmt.Sprintf("Open http://localhost:%d/debug/pprof/ for live profiling", args.NetProfile))
	}

	err = runMain(args, posArgs)
	if code := app.PrintLoadError(err); code != 0 {
		var lockedErr *lockfile.LockedError
		if errors.As(err, &lockedErr) {
			// runtree may be started without a terminal (e.g. from a desktop menu)
			gtkui.ShowError("The tree is locked", "The tree cannot be run now: "+lockedErr.Error())
		}
		os.Exit(code)
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
	runner.SetBinaryDir(args.RtrunnerDir)
	runner.SetSSHShare(!args.NoSSHShare)
	runTree, fullFilePath, warnings, err := app.LoadTree(posArgs[1], args.Runner, args.MaxDepth)
	if err != nil {
		return err
	}
	if args.Check {
		app.PrintCheck(posArgs[1], runTree, warnings)
		return nil
	}

	release, err := app.LockTree(args.Lock, runTree, fullFilePath)
	if err != nil {
		return err
	}
	defer release()

	var db *logdb.DB
	if args.LogDB != "" {
		db, err = app.OpenLogDB(args.LogDB, fullFilePath, runTree)
		if err != nil {
			return fmt.Errorf("could not open the log database: %w", err)
		}
		defer app.CloseLogDB(db)
		runTree.SetLogHandler(logging.Multi(slog.Default().Handler(), db.Handler()))
		runTree.SetOutputRecorder(app.LogDBRecorder(db))
	}
	if len(args.Thaw) > 0 {
		runTree.Lock("--thaw")
		err := runTree.ThawGates(true, args.Thaw)
		runTree.UnLock()
		if err != nil {
			return fmt.Errorf("--thaw: %w", err)
		}
	}
	handleTerm(runTree, db)

	err = gtkui.InitApplication(runTree, warnings)
	if err != nil {
		return fmt.Errorf("could not initialize application: %w", err)
	}
	if args.ExitOnFinish {
		gtkui.SetExitOnFinish()
	}
	if mode, ok := gtkui.ParseButtonMode(args.Buttons); ok {
		gtkui.SetButtonMode(mode)
	}
	if args.Appearance != "" {
		gtkui.SetAppearance(gtkui.Appearance(args.Appearance))
	}

	exitCode = gtkui.RunApplication()
	app.StopRunning(runTree)
	if args.ExitOnFinish && exitCode == 0 {
		runTree.RLock("exit code")
		exitCode = finishExitCode(runTree.Root.Status)
		runTree.RUnlock()
	}
	return nil
}
