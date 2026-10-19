* implement "if" node type
* implement NavBlocked navmode
* document "include" property, explain include/reduce
* possibly show a progress bar for each node, that shows the percentage of finished nodes
* create a special view that shows tree total time, tree total progress and processing speed graph?
* MAYBE after selecting a status filter in the navbar, show a graph for number of nodes for that status?
* play a sound when a node goes paused
* create a main menu, and add menu items to toggle the various windows above
* add extra actions for paused nodes to deal with siblings: cancel siblings, freeze siblings, thaw siblings, this is
  very low priority, you can do it on its parent...
* command line switch to exit app when the root node succeeds or fails
* runner sessions that survive the GUI: when the client disconnects (GUI closed or crashed, network error), keep
  the process running and buffer its recent output, so that a restarted GUI can reattach to it. Currently the
  runner terminates the process (SIGINT, then SIGTERM, then SIGKILL) when the client is gone. Needs a protocol
  change (bump `runner.Version`), a session registry on the runner, and restoring node statuses in the GUI.
* macOS support (built on a Mac, arm64, macOS 27, on 2026-10-19):
  * `scripts/build.py` builds everything on macOS, and `go test ./...` passes there (the local runner tests start
    real processes in ptys). Test rtrunner by hand too (process groups, signals).
  * runtree GUI: it builds with Homebrew's `gtk4` (4.24) and `vte3` (`vte-2.91-gtk4`, 0.84) after `brew install
    pkgconf gtk4 vte3 gobject-introspection socat`; the linker warns about duplicate libraries (`ld: warning: ignoring
    duplicate libraries`, harmless). It runs (light theme without `GTK_THEME=Adwaita:dark`). Fixed: it froze, because
    a contended `pthread_rwlock` hangs on macOS 27.0.1 when the waiting threads get signals (reported to Apple),
    and GLib's sources share one: goroutines no longer call GLib (`runOnMain`). gotk4 still unrefs objects from its
    finalizers with `g_main_context_invoke` (rare, but it can hit it; block the signals around it, or keep the objects
    referenced?). Needs to be checked: clipboard shortcuts (Cmd instead of Ctrl+Shift on macOS), middle click paste
    (macOS has no primary selection), the close confirmation on Cmd+Q, the terminal mark of the run nodes in
    the navigator (drawn by `termmark.go` now, the emoji had no glyph in the macOS fonts), a glitch in the "Tree (" text
    of the navigator.
  * CI: a build job on a macOS runner (e.g. `macos-15`, arm64) with the Homebrew packages.
* test on Windows: `runtree-headless`, `rtrunner` and `runtree-lsp` from `dist/windows/amd64/` on a real Windows (only
  tested under wine so far): the local runner (ConPTY), the output, exit codes, `--lock`, Ctrl+C, `timeout` (Ctrl+C
  typed into ConPTY; SIGTERM is not supported there, so its step is a runner error, and SIGKILL follows).
* test on macOS: `rtrunner`, `runtree-headless` and the GUI (see macOS support above) from `dist/darwin/arm64/`.
* confirm multi-line paste with the middle mouse button too (primary selection). Ctrl+Shift+V and the context menu
  already ask for confirmation, but middle click paste is handled inside vte, so the button 2 press must be handled
  by runtree (read the primary selection, confirm, then paste it with `PasteText`).

# Logging (plan, 2026-10-17)

Two kinds of logs, through one slog pipeline: diagnostic logs about runtree itself (scheduler, runner connections,
GUI), for developers, on stderr with `-v` / `-d`; and events about what happened to the tree (node started, exited,
status changed, manual operations, signals, on_error, provides, locks, runner errors), with `node` and `event`
attributes. Terminal input is never logged (passwords). A handler sends every record to several places: stderr
(filtered by `-v` / `-d`, optionally JSON), an in-memory store for the Log tab, and later SQLite.

1. Done: the `println`s are replaced, the log levels are fixed, the events and a per-node logger (`rt/eventlog.go`),
   `--log-format json`.
2. Done: the Log tab of the GUI (the events of the subtree, auto-scroll). Maybe later: filter by level or event,
   search.
3. Done: `--log-db file`: the events in SQLite (`internal/logdb`, `modernc.org/sqlite`), a run per runtree session,
   written in batches by a goroutine; `--show-runs`, `--show-events`.
4. Done: the terminal output of the nodes is saved in the same database, `--show-output`. Maybe later: show the
   previous runs and their output in the GUI; delete old runs (`--prune-log`?); compress the output.

# Found by the documentation audit (2026-10-18)

The README was checked against the code when the documentation was moved to docs/. Fixed since then: the on_error
sibling operation stopped running siblings in the middle, a single include lost the title, provides and requires of
the including node, the descriptions of several properties, Skip and Skip all of a frozen seq/par node, a seq/par node
whose sub-nodes are all frozen showed Waiting, a name in both `rlocks` and `xlocks` was not an error, the exit code
did not depend on the result of the tree (`runtree-headless` has one now). Still open:

* `~` is not expanded in `cwd` (expand it, or report it as an error?)

# Ideas (from the old README)

* an expression language for conditions (e.g. https://expr-lang.org/)
* a strict mode: an option to make the undefined variable warnings errors
* deadlock detectors for locks and for dependencies (`runtree-headless` stops when nothing can go on; the GUI could
  tell it too, e.g. "nothing can be started: these nodes are frozen / blocked")
* the default of on_error from a command line option (runtree-headless already fails the nodes that would pause)
* signals for a subtree with different runners (which signals can be sent?)
* a conditional node: run a condition node first, and the other sub-nodes depending on its result
* `load` nodes, custom nodes (create, change and delete sub-nodes on the fly)

# Runners (plan, 2026-10-18)

The `runner` property chooses where the processes of the nodes run:

| `runner:`                           | runner                                                     |
|-------------------------------------|------------------------------------------------------------|
| omitted, or `local`                 | the runner embedded in runtree (the default)               |
| `host:port`, `tcp://host:port`      | an `rtrunner` daemon (unauthenticated, for trusted networks) |
| `ssh://user@host:port`              | `ssh -T user@host rtrunner --stdio`, a runner per run node |

All of them speak the same protocol (version handshake, then smux with a pty and a command stream), and one
connection still runs exactly one command, so a runner per run node needs no protocol change.

1. Done: the embedded runner, the default: each client gets one end of a socket pair, a goroutine serves the other end.
   No `rtrunner` to start, no open port. When runtree exits (also on SIGTERM), it stops the scheduler, terminates the
   local processes as `rtrunner` does when its client is gone (SIGINT, SIGTERM, SIGKILL), and waits for them.
   `runtree --runner ADDRESS` changes the default runner (of the nodes that do not specify one); `runner: local` is
   always local.
2. Done: `rtrunner --stdio` serves one connection on stdin/stdout, and the `ssh://` runner starts it with the system
   `ssh` (`~/.ssh/config`, keys, agent, ProxyJump). A failed ssh connection is a start failure, not exit code 255;
   `envs` and `cwd` apply on the remote host; signals reach the remote process; the exit events have the remote exit
   code and CPU times. The nodes connect in the background (`Node.Start`, "Running, connecting" in the GUI).
   Done: when runtree exits, the processes of the ssh and TCP runners get SIGINT, SIGTERM, SIGKILL through the
   protocol, and runtree waits for them (`Tree.TerminateRemote`), so their exits are logged too.
3. Done: managed ssh runners (`ssh://host` without a path): runtree uploads its own `rtrunner` for the platform of
   the host (`uname`) to `~/.cache/runtree/rtrunner-<sha256>`, once for parallel starts, checked by its hash before
   it is renamed; the binaries are embedded (`build.py`, `-tags rtrunners`), `--rtrunner-dir` overrides them.
   Still to do:
   * Done: one shared SSH connection per host (`ControlMaster=auto`, `ControlPersist=60`, sockets in
     `$XDG_RUNTIME_DIR/runtree`; above `MaxSessions` ssh makes a new connection), `--no-ssh-share`
   * test the macOS binaries on a Mac (darwin amd64/arm64 are embedded but not tested; Gatekeeper, the ad-hoc
     signature of darwin/arm64, `uname -m` under Rosetta)
   * Windows hosts: the OpenSSH server of Windows starts `cmd.exe` (or PowerShell), not `sh`, so the start and upload
     scripts do not work there (sftp for the upload?), and the protocol over the stdin/stdout of a Windows ssh session
     needs testing. A manually installed `rtrunner.exe` with a path may work already: test it.
4. Later: runner sessions that survive the GUI (below) with a runner per node: when the connection drops, the
   runner keeps the process and a socket, and `rtrunner --attach ID` reattaches (like tmux/dtach). For the local
   runner it needs a detached runner process instead of the embedded one. Headless mode (below) can use the
   embedded runner, so a tree in CI is a single command.
5. Later, not urgent: remote locks, held by an rtrunner on a host (like `--lock`, but there), so that runtrees on
   different machines (e.g. on the laptops of several users) do not run the same tree, or use the same hosts, at the
   same time. E.g. a `lock` property with a runner and a path; the rtrunner holds the flock while the connection is
   open, so it is released when runtree or the connection is gone.

# GUI layout and navigation (from the user, 2026-10-17)

* redesign the properties tab (`NodeFrameProperites`, `nodeframeproperties.go`), so that it fits narrower screens.
  The rows of operation buttons in the header (`nodeframeheader.go`) are the widest part: with a large font they
  make the right side wider than the window (they could wrap, e.g. in a `gtk.FlowBox`).
* Done: the path in the header wraps, its nodes are links that show the node in the tree; the lists of the navigator
  have a "show in the tree" button; the Log tab has an End button.
* the GUI over VNC (2026-10-18): GTK's GL renderer without a GPU sends the whole window on every frame (18.5 MB/s
  idle, 195 MB/s with a streaming terminal, measured through a proxy); `GSK_RENDERER=cairo` sends the changed parts
  (docs/getting-started.md). The node rows and the header call GTK only when something changed. Next: make cairo the
  default when `GSK_RENDERER` is not set (after a test on the VNC server); maybe a `gtk.ListView` for the navigator,
  so that a big tree has widgets only for the rows on the screen.

# Appearance (from the user, 2026-10-17)

The GUI is designed for a dark theme (`GTK_THEME=Adwaita:dark` or Dracula, with `GDK_DPI_SCALE`); `main.css` has
about 40 hard coded colors.

* resize cursor (<->) on the dividers of the panes. GTK 4 sets `col-resize` / `row-resize` on the handle of a
  `gtk.Paned` (`mainpaned.go` uses wide handles); check why it does not show (cursor theme? CSS of the handle?), and
  set it with `SetCursorFromName` if needed.
* dark / light / system appearance, with high contrast in all of them: replace the hard coded colors of `main.css`
  with named colors (or `@media (prefers-color-scheme: ...)`, which GTK 4.20+ supports, 4.22 is installed), and
  set the color scheme in `gtk.Settings` (`gtk-interface-color-scheme`). "System" follows the desktop setting
  (org.freedesktop.appearance color-scheme of the settings portal). Choose it without taking screen space: a
  command line flag and an item in a small menu button (see the main menu above), saved in a settings file in
  `os.UserConfigDir()` (runtree has no settings file yet).

# Tree files: error positions and VS Code support (roadmap, 2026-10-17)

Keep YAML. A VS Code extension gets completion, hover and validation from a JSON Schema for free (Red Hat YAML
extension / yaml-language-server); the semantic checks need runtree's own parser, with positions.

1. Done: parser with positions (yaml.v3), structured errors (file:line:col, node path, include chain), "did you
   mean", `runtree --check`, reporting all errors, warnings for undefined variables.
2. Done: the node property table (`NodeProperties`), the generated JSON Schema (`runtree --schema`,
   `schema/runtree.schema.json`), and the VS Code extension in `editors/vscode` (schema for `*.rt.yaml`, problem
   matcher for `runtree --check`). Still to do: publish the extension (VS Code Marketplace / Open VSX), and use the
   value kinds of the table in the parser too (only the property names come from it now).
3. Done: the language server `runtree-lsp` (`internal/lsp`, without GTK): the errors and warnings of `--check` as you
   type (with the unsaved changes, in the included files too), go to definition for includes, hover on variables
   (their values in the nodes, and where they are set), completion of include arguments; the VS Code extension
   starts it. Still to do: find the includes of an object (references), and publish `runtree-lsp` binaries for
   Windows and macOS (it builds without cgo).

# From TODO comments in the source (2026-10-17)

* expression language for computed values (e.g. a `!expr` tag), probably with [Expr](https://expr-lang.org)
  (MIT license): concatenating lists, replacing in strings, conditionals; variables of any type (numbers converted
  to strings with their source text, e.g. `1.10`)
* navigator layout uses a hard-coded width of 1024 (`nodenavigator.go` `layoutList`, `layoutTree`), long node titles
  produce GTK warnings ("Trying to measure GtkBox for width of 1024, but it needs at least ...")
* GTK command line parameters cannot be passed to the application (`gtkapplication.go` `RunApplication`)
* recursive manual operations: allow them when they are allowed on the child nodes (`op.go` `PerformOp`)
* allow a run node with empty `args` when `args_prefix` is not empty (`parser.go` `SelfCheck`)
* cache the rlock root after building the tree, instead of walking up the tree every time (`deplock.go`
  `getRLockRoot`)
* max_proc is checked in `findRunnable` for every node type; move it to `findRunnableRun` if a node type is added
  that does not start a process (e.g. "if") (`scheduler.go`)
* the initial pty size comes from `TerminalHelper.GetWinSize` while calculating client args (`run.go`
  `calcClientArgs`, "looks nasty")

# Adoptability (from a review, 2026-09-16)

Unique combination worth keeping: seq/par tree + locks/deps, live interactive PTY per step,
pause-on-error -> manual fix -> resume automation, remote execution. No mainstream tool
(Ansible, CI DAGs, Rundeck/AWX, mprocs/overmind, tmux scripts) covers all four.

Ordered by impact:

* SSH transport for runners, instead of the unauthenticated TCP daemon on :5000: see "Runners" above.
* TUI frontend (non-GTK), so runtree works interactively over SSH, and on macOS/Windows (`runtree-headless` covers
  the unattended runs, the TUI would be its interactive sibling).
  GTK4+VTE+cgo is the biggest install/build barrier.
* Headless mode: done, `runtree-headless` (docs/headless.md, `internal/headless`, `rt.Tree.SetUnattended`): the
  output on stdout with the labels of the nodes, the events on stderr, on_error paused fails, the run stops when
  nothing runs and nothing can be started, exit codes 0 / 1 / 2 / 3 / 75 / 130 / 143. It also builds for Windows (the
  local runner works there: a loopback TCP pair instead of a socket pair; ConPTY finds the programs in the PATH, gets
  the whole command line, and is closed after the exit; tested under wine only). The `timeout` property stops the
  steps that wait for input or never exit. Next:
  * ssh runners on Windows (`startSSH` with the pipes of `ssh.exe`; Windows OpenSSH has no ControlMaster, so no
    connection sharing);
  * test it on a real Windows (and macOS);
  * maybe: `--thaw ID` to thaw approval gates from the command line; a `lock` property of the root node, so that a
    tree is always locked without `--lock`; `--lock-wait` to wait for the lock instead of exit 75; `--output none`
    (only the events, with `--log-db`).
* Better test coverage. The test suite (`go test ./...`, run by CI) covers the parser and its errors, vars
  substitution, the operations, reset / re-run and unprovide, the events, the scheduler (seq/par order, max_proc,
  frozen/skipped, requires, xlocks/rlocks, with real processes too), the runners (local, TCP, ssh with a fake ssh,
  uploads, a client without a terminal), the log database, the lock file (also the Windows one, under wine), the
  on_error sibling operations, the language server (also its edge cases: unknown methods, invalid params, unsaved and
  non-file documents, encoded paths), unattended trees and runtree-headless (the end of the run, its output, exit
  codes). Still thin: the GUI (no tests).
* Reduce DSL friction: avoid duplicating inventory via per-node `runner`; make it easy for
  a leaf node to call an existing Ansible playbook / script.
* Pitch: "`make -j` for ops, where every job is a terminal you can step into".
