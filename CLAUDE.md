# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`runtree` is a process scheduler: a YAML file defines a tree of nodes (`run` / `seq` / `par`), and a GTK4 GUI
(`runtree`) schedules the leaf commands on one or more `rtrunner` daemons, which execute them in pseudo-terminals
and stream the ptys back so they can be shown/interacted with in the GUI. README.md is the full user-facing spec
of the YAML format, node properties, statuses, and operations — consult it before changing parser or scheduler
semantics. TODO.md lists planned work.

Go module: `github.com/nagylzs/runtree` (Go 1.25+, cgo required for the GUI). Linux is the primary target;
`rtrunner` also builds on Windows and cross-compiles for macOS, `runtree` (GUI) does not build on Windows (VTE
dependency); see TODO.md for macOS.

## Build / run

System deps: gcc, gtk4, vte4 (`libgtk-4-dev libvte-2.91-gtk4-dev libgirepository1.0-dev` on Debian/Ubuntu),
plus `bash` and `socat` at runtime for the GUI (it spawns `socat` inside each VTE terminal to attach to the
forwarded pty socket).

```bash
python3 scripts/build.py          # official build: go mod tidy + both binaries (+ _debug variants) into dist/<os>/<arch>/, with version ldflags
go build -o runtree ./cmd/runtree # quick build without version info
go build -o rtrunner ./cmd/rtrunner
go build ./... && go vet ./...    # compile/vet everything (first GTK build takes several minutes)
```

Running locally requires a runner listening first; the default runner address is `127.0.0.1:5000`:

```bash
./rtrunner -v                     # listens on :5000 (-l to change); -d for debug logs
./runtree -v test/test01.rt.yaml  # GUI; --max-depth, --cpu-profile, --mem-profile, --net-profile-port also available
```

`test/*.rt.yaml` are hand-run example trees, not automated tests. Unit tests are `internal/rt/*_test.go`; shared helpers
are in `internal/rt/helpers_test.go` (`loadTestTree` builds a tree from YAML with the scheduler disabled, `setStatus`
sets a node status). Run them with `go test ./...`. CI (`.github/workflows/build.yml`) runs the tests and then
`scripts/build.py` on `main`. Coverage is still small: add tests for new and changed code.

Dark GTK theme is the design target: `GTK_THEME=Adwaita:dark`.

## Architecture

Three binaries; `runtree` and `rtrunner` share the runner protocol:

- `cmd/rtrunner` → `internal/runner` (server side). Accepts TCP connections; each connection becomes one
  `Server` that runs exactly one command in a pty (`server_unix.go` via creack/pty, `server_windows.go` via conpty).
- `cmd/runtree-lsp` → `internal/lsp`: language server for editors (stdio JSON-RPC, no dependencies, no GTK/cgo). It
  loads the trees with `rt` (open documents through `Sources.Overlay`) and publishes `ParseError`s as diagnostics;
  `editors/vscode/extension.js` starts it.
- `cmd/runtree` → `internal/rt` (tree model + scheduler) + `internal/gtkui` (GTK4 GUI). Each started node opens its
  own `runner.Client` TCP connection.

### Runner protocol (`internal/runner`)
After a version handshake (`proto.go`, `runner.Version` must match on both ends — bump it when changing the wire
format), the client sends `MsgStartPty` and the TCP connection is upgraded to an `smux` session with two streams:
a raw **pty stream** and a **command stream** (msgpack-encoded messages: resize, signal, proc-state, error).
On the client side the pty stream is re-exposed as a local Unix domain socket (`Client.PtyPath`); the GUI attaches a
VTE terminal to it with `socat`. Process exit is reported via `ProcState` → `onNotify` callback.

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
- `scheduler.go` — `Tree.Run` loop (ticks every `HZ`=1s or when `Awake()` is called; debounced via
  `internal/events.Debouncer`). Each cycle: commit elapsed times → `updateStatusesDown` → `updateBlockedAll` →
  `findRunnable` (depth-first; `seq` stops at the first unfinished child, `par` scans all) → `Node.Start`.
  Only one node is started per cycle; starting re-awakes the scheduler.
- `status.go` — status derivation. `Node.Status` must only be changed via `updateStatus`; run-node status comes from
  the client `ProcState`/`StartError`/manual status, par/seq status is computed from children. Status changes call
  `TerminalHelper.OnNodeChanged` and may trigger `on_error` sibling ops.
- `deplock.go` — `provides`/`requires` dependencies and `xlocks`/`rlocks` resource locks; computes `Node.Blocked`.
  Lock bookkeeping is tree-wide (`Tree.providedBy`, `xAcquiredBy`, …).
- `op.go` — manual operations (`PerformOp`, `AllowedManualOps`) invoked from the GUI; the scheduler never uses them.
- `terminal.go` — `TerminalHelper` interface: the only way `rt` talks to the GUI. `gtkui.RunTreeApp` implements it.

### Locking convention
The whole tree is guarded by one `sync.RWMutex` (`Tree.Lock/RLock`, with a `who` string for debugging deadlocks —
`PrintWho` runs every second from the GUI). Most `rt` methods take a `locked bool` parameter and `panic` if called
unlocked; callers are responsible for holding the tree lock. `schedule()` must be called *unlocked* (it locks itself).
Callbacks from runner goroutines (`procStateChanged`, `onError`) acquire the lock themselves. Keep this discipline
when adding methods.

### GUI (`internal/gtkui`)
Package-level globals `RunTree`, `Application`, `GtkApplication` (`global.go`). `RunTreeApp` owns the VTE terminals
(one per node id) and pushes updates via `glib.IdleAdd` from a 1s `runGuiUpdater` goroutine plus event-driven
`navigator.UpdateNode` calls. `mainpaned.go` splits into `nodenavigator.go` (tree list, `treenodeview.go` rows) and
`nodeframe*.go` (tabs for the selected node: header/actions, properties, vars, envs, deps, description, tools).
Styling is in the embedded `main.css`. All GTK calls must happen on the GTK main thread.

## Conventions

- Logging is `log/slog` with `tint`; `-v` → Info, `-d` → Debug. Stray `println` debugging calls exist in the
  scheduler; don't add more.
- New YAML node properties need: an entry in `NodeProperties` (`properties.go`: name, value kind, inherited, default,
  description; the accepted property names and the JSON Schema come from it), then regenerate the schema with
  `go test ./internal/rt/ -run TestSchemaFileIsUpToDate -update-schema` (a test fails if it is stale), parsing in
  `parser.go`, a `Parsed`/`Calculated` field
  in `node.go`, substitution/inheritance in `vars.go`, GUI display in the relevant `nodeframe*.go`, and a README entry.
- Windows/Unix split uses `//go:build` files in `internal/runner` and `internal/signal`.
