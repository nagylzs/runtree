# Getting started

## Requirements

runtree runs on Linux, and it builds on macOS too (the GUI is not tested there yet). The GUI (`runtree`) needs GTK 4
and VTE (the terminal widget of GNOME Terminal); the runner (`rtrunner`) is a static binary without dependencies, that
also runs on macOS and other Linux machines.
[`runtree-headless`](headless.md) runs trees without the GUI (e.g. in CI): it is a static binary too, that also runs
on Windows.

At runtime, the GUI needs `bash` and `socat` (each terminal attaches to the process with `socat`). runtree does not
start without `socat`.

## Installation

There are no packages yet: build it from source. The build needs Go 1.25 or newer (with cgo), gcc, pkg-config, the
GTK 4 and VTE development packages, git and Python 3. If the `golang` package of your distribution is older than
1.25, install Go from [go.dev](https://go.dev/dl/).

On Debian or Ubuntu:

```bash
sudo apt update
sudo apt install -y build-essential pkg-config golang git python3 socat \
    libgtk-4-dev libvte-2.91-gtk4-dev libgirepository1.0-dev
```

On Arch or Manjaro:

```bash
sudo pacman -S base-devel go git python socat gtk4 vte4
```

On macOS, with [Homebrew](https://brew.sh) (and the Xcode command line tools, `xcode-select --install`):

```bash
brew install go git pkgconf gtk4 vte3 gobject-introspection socat
```

Without GTK 4 and VTE (when `pkg-config` does not find `gtk4` and `vte-2.91-gtk4`), the build skips the GUI and builds
the other programs.

Then build it:

```bash
git clone https://github.com/nagylzs/runtree.git
cd runtree
python3 scripts/build.py
```

The first build takes several minutes (the GTK bindings are compiled once, then they are cached). The binaries are
put into `dist/<os>/<arch>/`, e.g. `dist/linux/amd64/`, or `dist/darwin/arm64/` on a Mac with Apple silicon:

| File | What it is |
|------|------------|
| `runtree` | the scheduler and its GUI |
| `runtree-headless` | the scheduler without the GUI, for [CI and cron](headless.md) |
| `rtrunner` | the runner daemon, for [runners](runners.md) on other machines |
| `runtree-lsp` | the language server for [editors](editors.md) |
| `*_debug` | the same, with debug information |

`dist/rtrunners/` has `rtrunner` for other platforms (Linux on x86_64, arm64 and 32 bit ARM, macOS). `runtree` and
`runtree-headless` have them inside too: they upload the right one to the hosts of
[ssh runners](runners.md#ssh-runners). `dist/windows/amd64/` has the programs that run on Windows: `runtree-headless`,
`rtrunner` and `runtree-lsp`.

`go build -o runtree ./cmd/runtree` builds it too, but without version information and without the `rtrunner`s for
the uploads. See [Development](development.md) for the details of the build.

Put `runtree` on your `PATH`, e.g. copy it to `~/bin` or `/usr/local/bin`.

## The first tree

A tree file is a YAML file with a top level `tree` object. Save this as `hello.rt.yaml`:

```yaml
tree:
  title: "Hello"
  seq:
    - title: "Say hello"
      args: [echo, "Hello from runtree!"]
    - title: "Wait a bit"
      args: [sleep, "3"]
```

Run it:

```bash
runtree hello.rt.yaml
```

The window shows the tree on the left. runtree starts the first step right away; when it finishes, it starts the
second one. Click a node to see its terminal and its properties on the right. When both steps have finished, the tree
is Success. Close the window to quit.

Things to try next:

- Make the first step fail: `args: [sh, -c, "echo failing; exit 1"]`. The seq node fails, and the second step is
  not started. Fix the command and run the tree again, or select the failed step and click **Re-run**.
- Change `seq` to `par`: both steps start at the same time.

The [examples](../examples/README.md) show the features one by one, from `01-hello` to a rolling upgrade of
web servers. They only run harmless commands, so you can run all of them:

```bash
runtree examples/03-parallel.rt.yaml
```

## Checking a tree

`runtree --check` loads a tree file (and the files it includes) and reports the errors and warnings, without starting
the GUI or any process:

```
$ runtree --check upgrade.rt.yaml
upgrade.rt.yaml:5:7: unknown property "on_eror" (did you mean "on_error"?)
  in node: upgrade › deploy
upgrade.rt.yaml:8:41: invalid args: item 4 is a number, expected a string (put it in quotes)
  in node: upgrade › reboot
2 errors
```

The exit code is 1 when there is an error. A tree without errors prints `upgrade.rt.yaml: OK, 3 nodes`.

See [Checking a tree file](tree-files.md#checking-a-tree-file). [Editors](editors.md) can show the same errors while
you type.

## Appearance

runtree is designed for dark GTK themes:

```bash
GTK_THEME=Adwaita:dark runtree tree.rt.yaml
```

It works with light themes too, but some parts may still look dark. On a HiDPI screen, `GDK_SCALE=2` doubles the
size. The application id is `com.github.nagylzs.runtree.gui`.

## A remote or virtual X server

runtree draws its window with GTK's cairo renderer, so it works well on an X server without a GPU (VNC, Xvfb, `ssh -X`)
too. GTK's own default is OpenGL; without a GPU it is software OpenGL, which sends the whole window to the X server on
every change (6.4 MB on a 1600×1000 screen, and it changes every second while nodes run). The cairo renderer sends only
the changed parts. Measured on an X server without shared memory (like one over the network), with a tree of 586
nodes:

| | OpenGL (`GSK_RENDERER=ngl`) | cairo (runtree's default) |
|---|---|---|
| idle, while nodes run | 18.5 MB/s | almost nothing |
| switching to the Tree view | 50 MB, then 25 MB/s | 6 MB |
| a terminal with streaming output | 195 MB/s | 4 MB/s |

`GSK_RENDERER` chooses another renderer, e.g. on a local desktop with a GPU (`ngl` or `vulkan`):

```bash
GSK_RENDERER=ngl runtree tree.rt.yaml
```

runtree sets `GSK_RENDERER` only for itself: the commands of the nodes do not get it (unless you set it). With `-d`,
runtree logs the frames of the window that take more than 100 ms ("slow frame").

## Next steps

- [Writing tree files](tree-files.md): the structure of the tree
- [How a tree runs](execution.md): what happens when a step fails
- [Runners](runners.md): run the commands on other machines over ssh
