# Command line

## runtree

```
runtree [OPTIONS] TREE_FILE
```

Loads the tree file and runs it in the GUI. Without errors in the tree, the window opens and the scheduler starts.

| Option | Description |
|--------|-------------|
| `-v`, `--verbose` | log the information messages (the events of the tree) on stderr |
| `-d`, `--debug` | log the debug messages too |
| `--log-format=text\|json` | the format of the log on stderr (`text` by default) |
| `--version` | print the version, and exit |
| `--check` | only check the tree file and the files it includes, and exit (see [checking](../tree-files.md#checking-a-tree-file)) |
| `--schema` | print the JSON Schema of tree files, and exit (see [editors](../editors.md)) |
| `--max-depth=N` | the maximum depth of the tree, and of the includes (30 by default) |
| `--runner=ADDRESS` | the runner of the nodes that do not specify one (`local` by default), see [runners](../runners.md#the-default-runner) |
| `--rtrunner-dir=DIR` | a directory of `rtrunner-<goos>-<goarch>` binaries to upload to the hosts of ssh runners, instead of the embedded ones (also `RUNTREE_RTRUNNER_DIR`) |
| `--no-ssh-share` | do not share one ssh connection for each host |
| `--lock=FILE` | lock FILE while the tree runs; another runtree with the same lock file exits with 75 (see [lock files](../practice.md#running-a-tree-once-at-a-time)) |
| `--log-db=FILE` | save the events and the terminal output in an SQLite database (see [saving the log](../logging.md#saving-the-log)) |
| `--show-runs` | list the runs of the `--log-db` database, and exit |
| `--show-events` | print the events of a run of the `--log-db` database, and exit |
| `--show-output=NODE` | print the terminal output of a node (id, title or path) from the `--log-db` database, and exit |
| `--run=ID` | the run for `--show-events` and `--show-output` (the last one by default) |
| `--start=N` | the start of the node for `--show-output`, 2 for the first re-run... (the last one by default) |
| `--cpu-profile=FILE` | write a CPU profile to FILE (for developers) |
| `--mem-profile=FILE` | write a memory profile to FILE when runtree exits (for developers) |
| `--net-profile-port=PORT` | serve live profiles on `http://localhost:PORT/debug/pprof/` (for developers) |
| `-h`, `--help` | show the options |

Exit codes: 0 when the window was closed, 1 when the tree or the command line has errors, 75 when the lock file is
locked, 143 on SIGTERM.

## rtrunner

```
rtrunner [OPTIONS]
```

The runner daemon: it listens on a TCP port, and runs the commands of the runtrees that connect to it (see
[rtrunner daemons](../runners.md#rtrunner-daemons)). The connections are not authenticated.

| Option | Description |
|--------|-------------|
| `-l`, `--listen=ADDRESS` | listen on ADDRESS (`:5000` by default: port 5000 on all interfaces) |
| `--stdio` | serve one runtree connection on stdin and stdout, then exit (ssh runners start it like this) |
| `--check-sha256=HEX` | check that the SHA-256 of the executable is HEX, then exit (after an upload) |
| `-v`, `--verbose` | log the information messages |
| `-d`, `--debug` | log the debug messages too |
| `--log-format=text\|json` | the format of the log on stderr |
| `--version` | print the version, and exit |
| `-h`, `--help` | show the options |

## runtree-lsp

```
runtree-lsp [--stdio] [--version]
```

The language server: it speaks the Language Server Protocol on stdin and stdout (see [editors](../editors.md)).
`--stdio` is accepted for the editors that pass it, `--version` (`-V`) prints the version.

## Environment variables

| Variable | Used for |
|----------|----------|
| `RUNTREE_RTRUNNER_DIR` | the same as `--rtrunner-dir` |
| `GTK_THEME` | the GTK theme of the GUI, e.g. `Adwaita:dark` |
| `GDK_SCALE` | the scale of the GUI, e.g. 2 on a HiDPI screen |
| `XDG_RUNTIME_DIR` | the directory of the shared ssh connections (`$XDG_RUNTIME_DIR/runtree`) |

## Files

| File | What it is |
|------|------------|
| `~/.cache/runtree/rtrunner-<sha256>` | on the hosts of managed ssh runners: the uploaded `rtrunner` |
| `$XDG_RUNTIME_DIR/runtree/` | on the machine of runtree: the sockets of the shared ssh connections |
