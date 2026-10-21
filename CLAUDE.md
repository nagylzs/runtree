# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`runtree` is a process scheduler: a YAML file defines a tree of nodes (`run` / `seq` / `par`), and a GTK4 GUI
(`runtree`) schedules the leaf commands on runners, which execute them in pseudo-terminals and stream the ptys back
so they can be shown/interacted with in the GUI. `runtree-headless` runs the same trees without the GUI (CI, cron). The default runner is embedded in `runtree` (`runner: local`); an
`rtrunner` daemon (`runner: host:port`) runs them on another machine. `docs/` is the user documentation (README.md
is the overview): the YAML format, node properties, statuses, operations, runners — consult it before changing parser
or scheduler semantics, and update it with the change. TODO.md lists planned work.

Go module: `github.com/nagylzs/runtree` (Go 1.25+, cgo required for the GUI). Linux is the primary target;
`rtrunner`, `runtree-headless` and `runtree-lsp` also build for Windows (`build.py` cross-builds them into
`dist/windows/amd64/`, or `build.py` on a Windows host; tested on Windows 11 and under wine, with a separate
`WINEPREFIX`) and macOS, `runtree` (GUI) does not build on Windows (VTE dependency); see TODO.md for Windows and macOS.

## Build / run

System deps: gcc, gtk4, vte4 (`libgtk-4-dev libvte-2.91-gtk4-dev libgirepository1.0-dev` on Debian/Ubuntu),
plus `bash` and `socat` at runtime for the GUI (it spawns `socat` inside each VTE terminal to attach to the
forwarded pty socket). On macOS: `brew install pkgconf gtk4 vte3 gobject-introspection socat`; without GTK/VTE
(pkg-config), `build.py` skips `runtree` and builds the others.

```bash
python3 scripts/build.py          # official build: go mod tidy + all binaries (+ _debug variants) into dist/<os>/<arch>/, with version ldflags
go build -o runtree ./cmd/runtree # quick build without version info
go build -o rtrunner ./cmd/rtrunner
go build ./... && go vet ./...    # compile/vet everything (first GTK build takes several minutes)
```

The default runner is the local one, embedded in `runtree`, so no `rtrunner` is needed for local trees:

```bash
./runtree -v examples/03-parallel.rt.yaml  # GUI; --max-depth, --cpu-profile, --mem-profile, --net-profile-port also available
./rtrunner -v                     # only for runner: host:port; listens on :5000 (-l to change); -d for debug logs
go build -o runtree-headless ./cmd/runtree-headless  # no GTK; exit code 0 success, 2 failed, 3 stopped (docs/headless.md)
```

`examples/*.rt.yaml` are example trees from simple to complex (examples/README.md lists them, `TestExampleTrees` loads
them). Unit tests are `internal/rt/*_test.go`; shared helpers
are in `internal/rt/helpers_test.go` (`loadTestTree` builds a tree from YAML with the scheduler disabled, `setStatus`
sets a node status; `ptyTerminalHelper` attaches to the ptys, for tests that start real processes, on the local runner
by default, or on `startTestRunner`, an in-process TCP runner on a free port; `killNode` stops them). Run them with
`go test ./...`. CI (`.github/workflows/build.yml`) runs the tests and then `scripts/build.py` on `main`. Coverage is
still small: add tests for new and changed code.

The GUI has a dark and a light palette (`--appearance`); dark is the default when the desktop has no preference.

## Architecture

Four binaries; `runtree`, `runtree-headless` and `rtrunner` share the runner protocol:

- `cmd/rtrunner` → `internal/runner` (server side). Accepts TCP connections; each connection becomes one
  `Server` that runs exactly one command in a pty (`server_unix.go` via creack/pty, `server_windows.go` via conpty).
  The same `Server` is the local runner in `runtree` (`local.go`): each connection is a socket pair, the server is a
  goroutine (on Windows: a loopback TCP pair). When `runtree` exits (`app.StopRunning`, also on SIGTERM), it stops the
  scheduler and terminates the local processes like `rtrunner` does when its client is gone (`runner.StopLocal`), and
  the remote ones through their connections (`Tree.TerminateRemote`). `rtrunner --stdio` serves one
  connection on stdin/stdout: the `ssh://` runner (`ssh.go`) starts it with the system `ssh` for each connection.
  Without a path the ssh runner is managed (`upload.go`, `binaries.go`): a start script runs the uploaded
  `rtrunner-<sha256>` for the platform of the host, or runtree uploads it first. The binaries are embedded only with
  `-tags rtrunners` (`internal/rtrunners`, `scripts/build.py` builds them into `internal/rtrunners/bin/`). The
  connections to a host share one ssh master (`sshOptions`, `controlDir`); the tests turn it off (`SetSSHShare`).
- `cmd/runtree-lsp` → `internal/lsp`: language server for editors (stdio JSON-RPC, no dependencies, no GTK/cgo). It
  loads the trees with `rt` (open documents through `Sources.Overlay`) and publishes `ParseError`s as diagnostics;
  `editors/vscode/extension.js` starts it.
- `cmd/runtree-headless` → `internal/headless`: runs the tree unattended (`rt.Tree.SetUnattended`, `unattended.go`: a
  node that would pause fails, the clients have no terminal (`Client.SetNoTerminal`), the run ends when the root is
  finished or when nothing runs and nothing can be started, `checkEnd`). The output of the nodes goes to stdout with
  their labels (`output.go`; `--output failed` holds the lines of a node until it finishes, `none` drops them), the
  events to stderr (`events.go`). `internal/app` is what it shares with `runtree` (loading the tree, `--lock` and the
  `lock` property, `--log-db`, `StopRunning`); `--thaw` is `rt.Tree.ThawGates` (`thaw.go`).
- `cmd/runtree` → `internal/rt` (tree model + scheduler) + `internal/gtkui` (GTK4 GUI). Each started node opens its
  own `runner.Client` connection; `runner.ParseAddress` parses the `runner` property (`local`, `host:port`,
  `tcp://host:port`, `ssh://[user@]host[:port][/path]`); `runtree --runner` sets the default (`Sources.DefaultRunner`).
  TODO.md "Runners" has the plan.

### Runner protocol (`internal/runner`)
After a version handshake (`proto.go`, `runner.Version` must match on both ends — bump it when changing the wire
format), the client sends `MsgStartPty` and the connection is upgraded to an `smux` session with two streams:
a raw **pty stream** and a **command stream** (msgpack-encoded messages: resize, signal (to the process group),
proc-state, error).
On the client side the pty stream is re-exposed as a local Unix domain socket (`Client.PtyPath`); the GUI attaches a
VTE terminal to it with `socat`. Process exit is reported via `ProcState` → `onNotify` callback. Then the client
sends `MsgDisconnect` and closes the connection; the server must never close it first (smux may drop what the client
has not read yet: the end of the output, the exit status), see `TestServerWaitsForTheClient`.

### Tree model and scheduler (`internal/rt`)
- `source.go` / `errors.go` — files are parsed with yaml.v3 into plain maps/lists (what the parser works with), and
  `Sources` records the position of every map, key, value and list item. Parser errors are `ParseError`s
  (`file:line:col: message`, node path, include chain); inside `LoadNode`, return `propErrorf(name, ...)` for an invalid
  property value and `keyError` for a key, `locateError` adds the position. `runtree --check` only loads the tree.
  Strings tagged `!raw` have their `{` replaced by `rawStartTag` (vars.go) until they are calculated (`unraw`).
- `parser.go` — YAML → `Node` tree. Handles `include` (subtrees from same/other files, "reduce" when a single include
  is loaded into the parent), `for_vars` (cartesian product expansion), `ifeq`/`ifneq`, type inference from
  `seq:`/`par:`/`run:` shorthand keys, `on_error` parsing, and `Node.SelfCheck`. Errors are not returned from
  `LoadNode`: they are reported to `Sources` (`report`, positioned via `propErrorf`/`itemErrorf`/`keyError`) and
  loading continues with a default value, so that all errors are found; `LoadTree` returns them as `ParseErrors`.
  Warnings (e.g. undefined variables, collected by `calculate`) go to `Sources.warn` / `Sources.Warnings()`. `AddTree` loads a file into the
  `allTrees` map keyed by absolute path; `ParseToDom` builds the tree from the top-level `tree` object.
- `vars.go` — `Node.calculate` performs variable substitution (`{name}` templates) producing `Node.Calculated` from
  `Node.Parsed`. Inheritance rules for `vars`/`def_vars`/`envs`/`args_prefix`/`cwd`/`runner` live here.
- `run.go` — `Node.Start` connects to the runner in the background (`connect`, without the tree lock); meanwhile the
  node is starting (`Starting`): Running without a client. `NProc` is counted by `updateStatus`, when a run node starts
  or stops running.
- `scheduler.go` — `Tree.Run` loop (ticks every `HZ`=1s or when `Awake()` is called; debounced via
  `internal/events.Debouncer`). Each cycle: commit elapsed times and stop the nodes that ran longer than their
  `timeout` (`timeout.go`: Ctrl+C typed into the pty with `Client.Type`, then SIGTERM and SIGKILL; the node fails) →
  `updateStatusesDown` → `updateBlockedAll` →
  `findRunnable` (depth-first; `seq` stops at the first unfinished child, `par` scans all) → `Node.Start`.
  Only one node is started per cycle; starting re-awakes the scheduler.
- `status.go` — status derivation. `Node.Status` must only be changed via `updateStatus`; run-node status comes from
  the client `ProcState`/`StartError`/manual status, par/seq status is computed from children. Status changes call
  `TerminalHelper.OnNodeChanged` and may trigger `on_error` sibling ops.
- `deplock.go` — `provides`/`requires` dependencies and `xlocks`/`rlocks` resource locks; computes `Node.Blocked`.
  Lock bookkeeping is tree-wide (`Tree.providedBy`, `xAcquiredBy`, …).
- `op.go` — manual operations (`PerformOp`, `AllowedManualOps`) invoked from the GUI; the scheduler never uses them.
- `terminal.go` — `TerminalHelper` interface: the only way `rt` talks to the GUI. `gtkui.RunTreeApp` implements it.
- `eventlog.go` — events (log records with `node` and `event` attributes), and `EventLog`, the in-memory store that
  the tree's slog handler fills (it has its own lock, it never waits for the tree lock).
- `internal/logdb` — `--log-db`: saves the events (a slog handler, combined with stderr by `logging.Multi`) and the
  terminal output (`rt.OutputRecorder`, fed by `runner.Client.SetOutput`) in SQLite, in batches from a goroutine;
  the reader and the `--show-*` printing are there too. Bump `schemaVersion` when changing the tables.
- `internal/lockfile` — `--lock FILE`: an `flock` lock (`LockFileEx` on Windows) that the kernel releases when runtree
  exits; the file has the pid and the holder (`app.LockTree`, exit code 75 when it is locked).

### Locking convention
The whole tree is guarded by one `sync.RWMutex` (`Tree.Lock/RLock`, with a `who` string for debugging deadlocks —
`PrintWho` runs every second from the GUI). Most `rt` methods take a `locked bool` parameter and `panic` if called
unlocked; callers are responsible for holding the tree lock. `schedule()` must be called *unlocked* (it locks itself).
Callbacks from runner goroutines (`procStateChanged`, `onError`) acquire the lock themselves. Keep this discipline
when adding methods.

### GUI (`internal/gtkui`)
Package-level globals `RunTree`, `Application`, `GtkApplication` (`global.go`). `RunTreeApp` owns the VTE terminals
(one per node id). The GUI is updated every second by timers of the main loop (`RunTreeApp.Start`, `NodeFrame`), and
by event-driven `navigator.UpdateNode` calls. `mainpaned.go` splits into `nodenavigator.go` (tree list,
`treenodeview.go` rows) and `nodeframe*.go` (the selected node: the header with the operation buttons, whose icons are
drawn in `opicons.go`, and the tabs: properties with the dependencies, log, description, vars, envs). Styling is in
the embedded `main.css`, with named colors (`@rt_*`) from the dark or light palette (`palette.go`) that fits the
theme in use (`appearance.go`: system/dark/light, the ☰ menu, saved in `settings.go`). All GTK calls must happen on
the GTK main thread. `RunApplication` sets `GSK_RENDERER=cairo` when it is not set (fast without a GPU, e.g. over
VNC), hidden from the node processes by `runner.HideEnv`.
Goroutines must not call GLib either, not even `glib.IdleAdd`/`TimeoutAdd`: they pass the function to `runOnMain`
(`mainqueue.go`: a queue and a pipe that the main loop watches). On macOS 27 a contended `pthread_rwlock` hangs when
signals arrive, and GLib's sources share one, so goroutines that added sources froze the GUI.

## Conventions

- Logging is `log/slog` (`internal/logging`: tint text or JSON on stderr; `-v` → Info, `-d` → Debug). Don't use
  `println`. In `rt`, log with the tree's logger (`t.logger`, or `n.log()` that adds the `node` attribute): its
  records at Info and above are kept in `Tree.EventLog()`, the GUI's Log tab (`nodeframelog.go`) shows them. Events
  of the tree have an `event` attribute (constants in `eventlog.go`, documented in docs/logging.md "Events"), and their
  message must be complete for humans: the Log tab shows only the message. Never log terminal input or `envs`.
- New YAML node properties need: an entry in `NodeProperties` (`properties.go`: name, value kind, inherited, default,
  description; the accepted property names, the JSON Schema and docs/reference/properties.md come from it), then
  regenerate them with `go test ./internal/rt/ -run 'TestSchemaFileIsUpToDate|TestPropertiesDocIsUpToDate'
  -update-schema -update-docs` (tests fail if they are stale; `propertyGuide` in `docs_test.go` needs its section of
  the guide), parsing in `parser.go`, a `Parsed`/`Calculated` field in `node.go`, substitution/inheritance in
  `vars.go`, GUI display in the relevant `nodeframe*.go`, and its description in the guide (docs/).
- Docs tests: `TestDocLinks` checks the relative links of README.md and docs/, `TestOptionsDocumented`
  (internal/config) that every option is in docs/reference/cli.md, `TestExampleTrees` the examples.
- Windows/Unix split uses `//go:build` files in `internal/runner` and `internal/signal`.
