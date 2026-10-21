* implement "if" node type
* document "include" property, explain include/reduce
* possibly show a progress bar for each node, that shows the percentage of finished nodes
* create a special view that shows tree total time, tree total progress and processing speed graph?
* MAYBE after selecting a status filter in the navbar, show a graph for number of nodes for that status?
* play a sound when a node goes paused
* create a main menu, and add menu items to toggle the various windows above
* add extra actions for paused nodes to deal with siblings: cancel siblings, freeze siblings, thaw siblings, this is
  very low priority, you can do it on its parent...
* runner sessions that survive the GUI: when the client disconnects (GUI closed or crashed, network error), keep
  the process running and buffer its recent output, so that a restarted GUI can reattach to it. Currently the
  runner terminates the process (SIGINT, then SIGTERM, then SIGKILL) when the client is gone. Needs a protocol
  change (bump `runner.Version`), a session registry on the runner, and restoring node statuses in the GUI.
* macOS support (built on a Mac, arm64, macOS 27, on 2026-10-19):
  * `scripts/build.py` builds everything on macOS, and `go test ./...` passes there (the local runner tests start
    real processes in ptys).
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
  * Done: CI builds and tests on a macOS runner (`macos-15`, arm64) with the Homebrew packages.
* test on Windows: tested on Windows 11 on 2026-10-21 (Go 1.26, `scripts/build.py` on the Windows host, `go test`
  without the GTK packages: the tests that need sh or the terminal socket are skipped there, `windows_test.go` files
  test the same with cmd and ping). `runtree-headless` works with cmd, PowerShell and ping nodes: the output, exit
  codes (0 / 2 / 130 / 75 / 1), on_error paused, `--output failed|none`, `timeout` (Ctrl+C typed into ConPTY stops
  ping; a process that ignores it is killed after the grace periods), Ctrl+C of runtree-headless (the local processes
  are killed at once; nothing is left after a hard kill of runtree-headless either), `--lock` and the `lock` property,
  `--check` (Windows paths, includes with `/` and `\`), `cwd`, `envs` (the system variables are replaced case
  insensitively), `--log-db`. `runtree-lsp` works with the URIs of VS Code (`file:///c%3A/...`, tested with a scripted
  client, not with VS Code itself). Fixed: build.py on a Windows host, the error of a command that cannot be started,
  a pseudo console with 0 columns or rows. `rtrunner.exe` over TCP works with the GUI on the Mac and on Linux: the
  terminals of the ConPTY processes in VTE (colors, Unicode, `Write-Progress`), typing into cmd and PowerShell, resize,
  signals (SIGKILL ends the process with exit code 1), closing the GUI while they run. (`exit` without a code in cmd
  exits with the last ERRORLEVEL, e.g. 9009 after a command that was not found.) The `ssh://` runner works from
  Windows (`startSSH` with the pipes of `ssh.exe`, without connection sharing: the ssh of Windows (OpenSSH 9.5) fails
  with "getsockname failed: Not a socket" with `ControlMaster`): `runtree-headless` to the Mac with the managed upload,
  exit codes, start errors, `timeout`, Ctrl+C with remote nodes (`Tree.TerminateRemote`). Fixed: an rtrunner started
  by sshd ignores Ctrl+C, and its processes inherited that (a `timeout` did not stop them); SIGINT is Ctrl+C typed
  into the pseudo console, and a `timeout` or the exit of runtree does not wait after a signal that the runner cannot
  send (SIGTERM on Windows: SIGKILL at once, 10 s at most instead of 20). Left:
  * the command lines are quoted for a Unix shell also for a Windows runner: the CmdLine property (`shellEscape`) and
    the start event (`quoteArgs`, it doubles the backslashes); copied into cmd or PowerShell, they do not work;
  * the ConPTY control sequences in the output of runtree-headless: each node starts with `ESC[?9001h ESC[?1004h
    ESC[?25l ESC[2J ESC[m ESC[H` and the title of the console (`ESC]0;C:\WINDOWS\system32\cmd.exe BEL`), and ends
    with `ESC[?25h ESC[?9001l ESC[?1004l`, often on a line of its own; blank lines become cursor moves (`ESC[7;1H`),
    so two lines can be on one labeled line. Filter them on Windows: drop the private modes, the title and the
    clear/home at the start, and make a newline of a move to column 1;
  * show NTSTATUS exit codes in hex on Windows: Ctrl+C makes 3221225786, that is 0xC000013A (STATUS_CONTROL_C_EXIT);
  * the log lines of the local runner have another format than the events in runtree-headless's stderr
    (`Oct 21 08:25:24.500 WRN terminating runner=local ...` among `08:25:24 WARN ...`);
  * the holder in the lock file is `HTPC\nagyl@htpc` (os/user gives DOMAIN\user on Windows).
* test on macOS: tested on 2026-10-21 from `dist/darwin/arm64/`: the GUI with a Windows `rtrunner` over TCP,
  `runtree-headless`, and `rtrunner` as a managed ssh runner (see Runners). Left: the GUI checks of "macOS support"
  above.

# Releases (decided 2026-10-21)

* Not in the Mac App Store: its apps must use the App Sandbox, and the processes that a sandboxed app starts are
  sandboxed too (no access to the user's files, ssh keys and Homebrew tools), while runtree runs any command of the
  tree; App Review is wary of apps that run code that is not in them, and GTK, VTE and socat would have to be bundled
  in a signed `.app`. The users are sysadmins and developers, they install such tools with Homebrew.
* macOS: a Homebrew tap first (`nagylzs/homebrew-runtree`, `brew install nagylzs/runtree/runtree`), with a formula
  that builds from source with `scripts/build.py` (so the managed ssh runner gets its embedded `rtrunner`s):
  `depends_on "go" => :build`, `pkgconf` and `gobject-introspection` to build, `gtk4`, `vte3`, `socat`. Maybe bottles
  built by a GitHub Actions job of the tap (the first build of the GTK bindings takes minutes). Later `homebrew-core`,
  when runtree is notable enough for it. A formula, not a cask: a cask installs a prebuilt `.app`, that needs a
  Developer ID signature and notarization.
* Linux, the primary target: packages at about the same time, e.g. an AUR package (Arch, Manjaro) and a `.deb`
  (Debian, Ubuntu).
* Done (2026-10-21): version numbers from the git tags (`--version`, docs/development.md "Versions and releases"),
  a release workflow that publishes the archives of a tag (Linux, macOS, Windows, the rtrunners, SHA256SUMS), a CI
  job on macOS. Left before the Homebrew formula: the macOS items above.

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
did not depend on the result of the tree (`runtree-headless` has one now), a `cwd` with `~` failed only when the node
started (`~` is not expanded, it is a load error now).

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
     `$XDG_RUNTIME_DIR/runtree`; above `MaxSessions` ssh makes a new connection), `--no-ssh-share`; not on
     Windows (no `ControlMaster`)
   * darwin/arm64 is tested (2026-10-21, from Linux to a Mac with macOS 27): the upload, 12 parallel nodes,
     `timeout`, `Tree.TerminateRemote`. The binary has the ad-hoc signature of the linker, and a file uploaded over ssh
     has no quarantine attribute, so Gatekeeper does not check it. Not tested: darwin/amd64, `uname -m` under Rosetta
   * Windows hosts: `rtrunner.exe` with a path works (`ssh://user@host/C:/path/to/rtrunner.exe`, tested on 2026-10-21
     with Windows 11). Managed runners do not: the OpenSSH server of Windows starts `cmd.exe` (or PowerShell), not
     `sh`, so the start and upload scripts do not work there (sftp for the upload?).
4. Later: runner sessions that survive the GUI (below) with a runner per node: when the connection drops, the
   runner keeps the process and a socket, and `rtrunner --attach ID` reattaches (like tmux/dtach). For the local
   runner it needs a detached runner process instead of the embedded one. Headless mode (below) can use the
   embedded runner, so a tree in CI is a single command.
5. Later, not urgent: remote locks, held by an rtrunner on a host (like `--lock`, but there), so that runtrees on
   different machines (e.g. on the laptops of several users) do not run the same tree, or use the same hosts, at the
   same time. E.g. a `lock` property with a runner and a path; the rtrunner holds the flock while the connection is
   open, so it is released when runtree or the connection is gone.

# GUI layout and navigation (from the user, 2026-10-17)

* Done (2026-10-21), so that the GUI fits narrow panes (e.g. half of a 1440 logical px screen with `GDK_SCALE=2`):
  the header has one row of operation buttons with drawn icons (`opicons.go`), only the icons when the texts do not
  fit (`--buttons`), the operations of the sub-nodes in the popover of **All**, the signals in a grid; the navigation
  modes are in the popover of one menu button; the tabs scroll; the properties tab is one column of names and values
  (the most looked-for first, the details with Show all), with the dependencies (the Dependencies tab is merged into
  it). (A `gtk.FlowBox` was tried for the buttons and dropped: it lines its lines up in columns, so it wrapped where
  one row would fit.) Still wide: a header row narrower than the 8 icons (about 260 px) is clipped.
* Done: the path in the header wraps, its nodes are links that show the node in the tree; the lists of the navigator
  have a "show in the tree" button; the Log tab has an End button.
* the GUI over VNC (2026-10-18): GTK's GL renderer without a GPU sends the whole window on every frame (18.5 MB/s
  idle, 195 MB/s with a streaming terminal, measured through a proxy); the cairo renderer sends the changed parts
  (docs/getting-started.md), it is runtree's default when `GSK_RENDERER` is not set. The node rows and the header call
  GTK only when something changed. Next: maybe a `gtk.ListView` for the navigator, so that a big tree has widgets only
  for the rows on the screen.

# Appearance (from the user, 2026-10-17)

The GUI is designed for a dark theme (`GTK_THEME=Adwaita:dark` or Dracula, with `GDK_DPI_SCALE`); `main.css` has
about 40 hard coded colors.

* resize cursor (<->) on the dividers of the panes. GTK 4 sets `col-resize` / `row-resize` on the handle of a
  `gtk.Paned` (`mainpaned.go` uses wide handles); check why it does not show (cursor theme? CSS of the handle?), and
  set it with `SetCursorFromName` if needed.
* Done (2026-10-21): dark / light / system appearance (`appearance.go`): the colors of `main.css` are named, the
  dark and the light palette (`palette.go`) define them, and the one that fits the theme in use is loaded (from its
  text color, so `GTK_THEME` works too); the terminals get light colors with a light theme. System reads the
  settings portal (Linux) or `AppleInterfaceStyle` (macOS), dark when unknown. The ☰ menu chooses it and the button
  mode, saved in `runtree/settings.json` (`os.UserConfigDir()`); `--appearance`, `--buttons`. Left: high contrast
  (`prefers-contrast`, GTK 4.20), and maybe `@media (prefers-color-scheme)` instead of reloading the palette when
  GTK 4.20 is common.

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
* navigator layout uses a hard-coded width of 1024 (`nodenavigator.go` `navigatorRowWidth`, a row with a long title
  is wider): the rows could follow the width of the navigator
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

* Done: SSH transport for runners, instead of the unauthenticated TCP daemon on :5000 (see "Runners" above).
* TUI frontend (non-GTK), so runtree works interactively over SSH, and on macOS/Windows (`runtree-headless` covers
  the unattended runs, the TUI would be its interactive sibling).
  GTK4+VTE+cgo is the biggest install/build barrier.
* Headless mode: done, `runtree-headless` (docs/headless.md, `internal/headless`, `rt.Tree.SetUnattended`): the
  output on stdout with the labels of the nodes, the events on stderr, on_error paused fails, the run stops when
  nothing runs and nothing can be started, exit codes 0 / 1 / 2 / 3 / 75 / 130 / 143. It also builds for Windows (the
  local runner works there: a loopback TCP pair instead of a socket pair; ConPTY finds the programs in the PATH, gets
  the whole command line, and is closed after the exit; tested on Windows 11, see "test on Windows" above). The
  `timeout` property stops the steps that wait for input or never exit. Next:
  * Done: `--thaw NODE` opens approval gates (also in the GUI), the `lock` property of the root node locks the tree
    without `--lock` (the GUI shows the holder in a window), `--output failed` and `--output none`, the ssh
    runners on Windows.
  * maybe: `--lock-wait [DURATION]` to wait for the lock instead of exit 75 (for queued CI runs).
* Better test coverage. The test suite (`go test ./...`, run by CI) covers the parser and its errors, vars
  substitution, the operations, reset / re-run and unprovide, the events, the scheduler (seq/par order, max_proc,
  frozen/skipped, requires, xlocks/rlocks, with real processes too), the runners (local, TCP, ssh with a fake ssh,
  also on Windows, uploads, a client without a terminal, ConPTY on Windows), the log database, the lock file (also the
  Windows one), the on_error sibling operations, the language server (also its edge cases: unknown methods, invalid
  params, unsaved and non-file documents, encoded paths), unattended trees and runtree-headless (the end of the run,
  its output, exit codes). Still thin: the GUI (no tests).
* Reduce DSL friction: avoid duplicating inventory via per-node `runner`; make it easy for
  a leaf node to call an existing Ansible playbook / script.
* Pitch: "`make -j` for ops, where every job is a terminal you can step into".
