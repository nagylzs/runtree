# Development

## Building

```bash
python3 scripts/build.py        # the official build
go build ./... && go vet ./...  # compile and vet everything
go build -o runtree ./cmd/runtree
```

`scripts/build.py` (it needs git, and a Linux, macOS or Windows host, on amd64 or arm64):

1. runs `go mod tidy`;
2. cross-builds `rtrunner` for the platforms of the [managed ssh runners](runners.md#managed-rtrunner) (linux amd64,
   arm64, arm with GOARM=6, darwin amd64, arm64) into `dist/rtrunners/`, and gzips them into
   `internal/rtrunners/bin/` (ignored by git);
3. builds `rtrunner`, `runtree`, `runtree-headless` and `runtree-lsp` into `dist/<os>/<arch>/`, with the version, the
   commit and the build time in them (`--version`), and `*_debug` variants with debug information. `runtree` and
   `runtree-headless` are built with `-tags rtrunners`, so they embed the binaries of step 2. `runtree` is built only
   when `pkg-config` finds GTK 4 and VTE (`gtk4`, `vte-2.91-gtk4`);
4. on Linux and macOS, cross-builds the programs without cgo for Windows too, into `dist/windows/amd64/`.

Only `runtree` needs cgo (GTK 4 and VTE through [gotk4](https://github.com/diamondburned/gotk4)); the others are built
without cgo, as static binaries. The first build of `runtree` takes several minutes, the cgo packages are cached after
it. `runtree` does not build on Windows (VTE), the others do (the processes run in ConPTY there).

## Tests

```bash
go test ./...            # all tests
go test -race ./...      # with the race detector
```

The tests do not need a display. The ones that start processes use real pseudo-terminals, on the local runner, on an
in-process TCP runner, or through a fake ssh (the test binary works as `rtrunner`). Add tests for new and changed
code.

Some files are generated, and a test fails when they are out of date:

| File | Regenerate with |
|------|-----------------|
| `schema/runtree.schema.json` | `go test ./internal/rt/ -run TestSchemaFileIsUpToDate -update-schema` |
| `docs/reference/properties.md` | `go test ./internal/rt/ -run TestPropertiesDocIsUpToDate -update-docs` |

Other tests check the documentation: every command line option is in `docs/reference/cli.md`, every example tree
loads without warnings, and the relative links of the documentation point to existing files and headings.

CI (GitHub Actions, `.github/workflows/build.yml`) runs the tests and `scripts/build.py` on every push to `main`, and
keeps the Linux and Windows binaries and `dist/rtrunners/` as artifacts.

The Windows code is tested by hand under [wine](https://www.winehq.org/), e.g.
`GOOS=windows go test -c -o lockfile.test.exe ./internal/lockfile && wine lockfile.test.exe -test.v` (a separate
`WINEPREFIX` keeps it away from your own wine).

## Structure

| Package | What it is |
|---------|------------|
| `cmd/runtree` | the GUI program: options, `--show-*`, exit |
| `cmd/runtree-headless` | the program without the GUI |
| `cmd/rtrunner` | the runner daemon (`--listen`), and `--stdio` for ssh runners |
| `cmd/runtree-lsp` | the language server |
| `internal/rt` | the tree: parser, variables, statuses, scheduler, operations, dependencies and locks, events |
| `internal/runner` | the runner protocol: client, server, the local runner, ssh runners and uploads |
| `internal/gtkui` | the GTK 4 GUI |
| `internal/headless` | `runtree-headless`: the run, its output on stdout, the events on stderr, the summary |
| `internal/app` | what the two programs share: loading the tree, the lock file, the log database, stopping the processes |
| `internal/rtrunners` | the embedded `rtrunner` binaries (`-tags rtrunners`) |
| `internal/logdb` | the SQLite log database (`--log-db`) |
| `internal/lockfile` | the lock file (`--lock`) |
| `internal/lsp` | the language server |
| `internal/logging` | the slog setup |
| `internal/config` | the command line options |

### The tree

YAML files are parsed with yaml.v3 into plain maps and lists, with the position of every key and value, so that the
errors have positions. The parser (`parser.go`) builds the nodes, expands `include` and `for_vars`, and calculates
the variables and the other properties of each node once (`vars.go`). The table of the node properties
(`properties.go`) is the single source of the property names: the parser accepts these names, and the JSON Schema and
the [properties reference](reference/properties.md) are generated from it.

The scheduler (`scheduler.go`) runs in cycles: it updates the statuses (`status.go`: run nodes from their processes,
seq and par nodes from their sub-nodes), the blocked flags (`deplock.go`), and starts one runnable node. A start
connects to the runner in the background (`run.go`). The whole tree is guarded by one `RWMutex`; most methods take a
`locked bool` parameter and panic when they are called without the lock.

### The runners

A runner connection runs exactly one command. After a version handshake (`runner.Version` must match on both ends),
the client sends the command, and the connection becomes an [smux](https://github.com/xtaci/smux) session with two
streams: the raw pty stream, and a command stream (msgpack messages: resize, signal, process state, error). The client
exposes the pty stream as a local Unix socket, and the GUI attaches a VTE terminal to it with `socat`. Without a
terminal (`runtree-headless`), the client copies the output to a writer instead.

The connection is a socket pair to an in-process server for the local runner (a loopback TCP connection on Windows),
a TCP connection to an `rtrunner` daemon, or the standard input and output of `ssh host rtrunner --stdio` for ssh
runners.

An unattended tree (`Tree.SetUnattended`, `internal/rt/unattended.go`) is the tree of `runtree-headless`: a node that
would pause fails, and the run ends when the root is finished, or when nothing runs and nothing can be started
(`checkEnd`).

## Adding a node property

1. Add it to `NodeProperties` in `internal/rt/properties.go` (name, value kind, inherited, default, description).
2. Parse it in `parser.go`, store it in `Parsed` / `Calculated` (`node.go`), substitute and inherit it in `vars.go`.
3. Show it in the GUI (`internal/gtkui/nodeframe*.go`).
4. Regenerate the schema and the properties reference (see above), and document it in the guide.
