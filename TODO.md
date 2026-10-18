* implement "if" node type
* implement NavBlocked navmode
* document "include" property, explain include/reduce
* show requires/provides on the dependencies tab
* possibly show a progress bar for each node, that shows the percentage of finished nodes
* create a special view that shows tree total time, tree total progress and processing speed graph?
* MAYBE after selecting a status filter in the navbar, show a graph for number of nodes for that status?
* show provides and requires under dependencies
* play a sound when a node goes paused
* create a main menu, and add menu items to toggle the various windows above
* add extra actions for paused nodes to deal with siblings: cancel siblings, freeze siblings, thaw siblings, this is
  very low priority, you can do it on its parent...
* command line switch to exit app when the root node succeeds or fails
* display CWD in properties
* runner sessions that survive the GUI: when the client disconnects (GUI closed or crashed, network error), keep
  the process running and buffer its recent output, so that a restarted GUI can reattach to it. Currently the
  runner terminates the process (SIGINT, then SIGTERM, then SIGKILL) when the client is gone. Needs a protocol
  change (bump `runner.Version`), a session registry on the runner, and restoring node statuses in the GUI.
* macOS support (checked on 2026-10-17, not tested on a Mac yet):
  * rtrunner: it cross-compiles for darwin/arm64 and darwin/amd64 (`internal/signal/signals_unix.go` is used on
    macOS too). Test it on a Mac (ptys, process groups, signals).
  * runtree GUI: Homebrew has `gtk4` (4.24) and `vte3` built with `-Dgtk4=true` (`vte-2.91-gtk4`, 0.84), and
    gotk4/gotkit have no OS specific code (cgo with pkg-config), so it should build after
    `brew install pkgconf gtk4 vte3 socat`. Needs to be checked: clipboard shortcuts (Cmd instead of Ctrl+Shift on
    macOS), middle click paste (macOS has no primary selection), the close confirmation on Cmd+Q.
  * CI: a build job on a macOS runner (e.g. `macos-15`, arm64) with the Homebrew packages.
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
   Still to do: when runtree exits, terminate the processes of the ssh and TCP runners through the protocol and wait
   for them (like the local ones), so their exits are logged too; now the rtrunner terminates them when runtree is
   gone, without logging.
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

# GUI layout and navigation (from the user, 2026-10-17)

* redesign the properties tab (`NodeFrameProperites`, `nodeframeproperties.go`), so that it fits narrower screens
* the path of the selected node (in the header, `nodeframeheader.go`) should wrap as needed, and its elements should
  be clickable: a click selects that node, and jumps to it in the tree
* in the navigator (`nodenavigator.go`), when the view mode is not the tree (Waiting, Failed, ...), the node cards
  should have a clickable "jump to" icon: it switches to the tree view mode, selects the node and makes it visible
  (expands its parents, scrolls to it)
* Log tab: a button in the top right corner that scrolls down to the end, and starts following the end of the log
  again

# Appearance (from the user, 2026-10-17)

The GUI is designed for a dark theme (`GTK_THEME=Adwaita:dark` or Dracula, with `GDK_DPI_SCALE`); `main.css` has
about 40 hard coded colors.

* resize cursor (<->) on the dividers of the panes. GTK 4 sets `col-resize` / `row-resize` on the handle of a
  `gtk.Paned` (`mainpaned.go` uses wide handles); check why it does not show (cursor theme? CSS of the handle?), and
  set it with `SetCursorFromName` if needed.
* the terminal mark of run nodes (`rt.TermEmoji` "🖵", shown in the status label in `treenodeview.go`) needs a font
  with that symbol. Use an embedded symbolic SVG icon (a `gtk.Image` before the status text, colored by the theme)
  instead, so that it does not depend on fonts or icon themes.
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
* TUI frontend (non-GTK), so runtree works headless, over SSH, and on macOS/Windows.
  GTK4+VTE+cgo is the biggest install/build barrier.
* Headless mode: exit with root node's status (already listed above), so it can double as a CI step. With `--lock`
  (done), users who run the same tree on a deploy host over ssh do not run it twice. Maybe: a `lock` property of the
  root node, so that a tree is always locked without `--lock`; `--lock-wait` to wait for the lock instead of exit 75.
* Better test coverage. The test suite (`go test ./...`, run by CI) covers the parser and its errors, vars
  substitution, the operations, reset / re-run and unprovide, the events, and starting processes on an in-process
  runner. Still thin: the scheduler loop (`findRunnable`, `max_proc`), lock/deps semantics, and the runner package.
* Reduce DSL friction: avoid duplicating inventory via per-node `runner`; make it easy for
  a leaf node to call an existing Ansible playbook / script.
* Pitch: "`make -j` for ops, where every job is a terminal you can step into".
