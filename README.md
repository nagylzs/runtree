# Introduction

The `runtree` utility is a scheduler. It runs multiple processes defined by a tree structure and displays
the state of the tree in a GUI. In the simplest case, each leaf node executes a predefined, fixed
shell command. Some processes can run in parallel, some must run sequentially, and some of them cannot be running at
the same time. Using a configuration file, you can specify that tree structure. It contains information about the
commands to be started, their environments, order of execution, synchronization, error handling, etc. The tree structure
can also contain nodes that pause execution and require user interaction to continue. When `runtree` is running, the
actual activity is a combination of the automatically scheduled activity and user activity (manual intervention).

The goal of `runtree` is to minimize the time needed to perform complex system administration and software deployment
tasks by maximizing parallel execution, while making it easy to supervise (view, pause, resume, debug) them.

# License

`runtree` is distributed under GNU GPLv3.

# How to build

## Prerequisites

* gcc
* gtk4 libs
* vte4 libs
* golang with cgo enabled
* python3 (only for build scripts)


On Arch/Manjaro:

```bash
sudo pacman -S base-devel extra/go extra/gtk4 extra/vte4
```

On Ubuntu/Debian:

```bash
sudo apt update && sudo apt install -y build-essential golang libgtk-4-dev libvte-2.91-gtk4-dev libgirepository1.0-dev
```

## Clone and build

The first build of runtree can take several minutes.

```bash
git clone git@gitlab.mess.hu:devops/runtree.git
cd runtree
python scripts/build.py
```
Alternatively, you can build this way:

```bash
go build -o runtree ./cmd/runtree
go build -o rtrunner ./cmd/rtrunner
```

but in that case, version information will not be included in the binary.

## Checking a tree file

`runtree --check tree.yaml` loads the tree file and the files that it includes, and exits without starting the GUI
or any process. It prints all errors with their positions, and the exit code is 1 when there is an error:

```
servers.yaml:7:7: unknown property "on_eror" (did you mean "on_error"?)
  in node: upgrade all › upgrade servers › deploy
  included from main.yaml:5:17 (include "servers.yaml|upgrade")
servers.yaml:12:16: invalid args: item 2 is a number, expected a string (put it in quotes)
  in node: upgrade all › upgrade servers › reboot
  included from main.yaml:5:17 (include "servers.yaml|upgrade")
2 errors
```

The first line of each error uses the usual `file:line:column: message` format, so editors (e.g. a VS Code task) can
jump to it. The errors are sorted by their position (the files in the order they are loaded). An error that is found
several times (e.g. in an object that is included more than once, or under `for_vars`) is reported once, with
"(found N times)" and the include chain of its first occurrence.

Warnings are printed the same way, with `warning:` before the message, but they do not prevent loading the tree. When
the GUI is started, they are printed too, and a banner above the tree tells that there are warnings (its "Show
warnings" button lists them):

```
lib.yaml:2:15: warning: undefined variable {user}, it is not set where it is included
  in node: deploy › job
  included from main.yaml:6:16 (include "lib.yaml|job")
main.yaml: OK, 5 nodes, 1 warning
```

The warnings are:

- `undefined variable {name}`: a value refers to a variable that is not set for the node. The reference is left as it
  is, e.g. the command gets `{user}@host` as an argument. In an included object it usually means that the variable
  should be set where the object is included (with `vars` or `for_vars` of a parent node, or with `def_vars` of the
  object). Only `{name}` references are checked, where the name is like an identifier (`[A-Za-z_][A-Za-z0-9_-]*`)
  and it is not after a `$` (`${name}` is a shell variable). Other text in curly braces, like `{}`, `{a,b}` or
  `{{.Name}}`, is not reported. It is only a warning, because a command may need a `{name}` text (e.g.
  `jq '{name}'`): tag such strings with [`!raw`](#raw-strings).
- `for_vars: name is an empty list`: there are no combinations, so the node has no sub-nodes.

## Logs

`runtree` and `rtrunner` log to stderr: only the warnings and the errors by default, `-v` adds the information
messages, and `-d` the debug messages too. `--log-format json` writes a JSON object per line instead of text (e.g.
for `jq`).

With `-v`, runtree logs what happens to the tree. These log records have an `event` attribute, and a `node`
attribute (the id of the node) when they are about a node:

| Event               | Level         | Logged when                                                                  |
|---------------------|---------------|------------------------------------------------------------------------------|
| `scheduler-started` | info          | the scheduler is started                                                     |
| `start`             | info          | a command is started on a runner (`start`, `runner`, `args`, `cwd`) (1)      |
| `start-failed`      | error         | it could not be started (e.g. the runner is not available)                   |
| `exited`            | info / warn   | the process exited (`start`, `pid`, `exit_code`, `error`, `elapsed`) (2)     |
| `status`            | info / warn   | the status of a node changes (`from`, `to`), warn for failed and paused      |
| `operation`         | info / warn   | an operation is performed from the GUI (`op`, `recursive`), warn on failure  |
| `signal`            | info / warn   | a signal is sent from the GUI (`signal`)                                     |
| `on-error`          | info          | the `on_error` operation is performed on the siblings (`op`)                 |
| `provided`          | info          | a node provides resources (`resources`)                                      |
| `unprovided`        | info          | resources are not provided anymore, by a reset to waiting or a re-run        |
| `locked`            | info          | a node acquires its `xlocks` and `rlocks`                                    |
| `unlocked`          | info          | it releases them                                                             |
| `runner-error`      | error         | a runner reports an error (`code`)                                           |

- (1) `start` is 1 for the first start of the node, 2 for the first re-run, and so on.
- (2) warn when the process failed (it did not exit with a success exit code).

The Log tab of the GUI shows these messages (and the other messages about the node, e.g. when the connection to the
runner is lost) for the selected node and the nodes below it. The root node shows the messages about the tree too. The
GUI keeps the last 100 000 messages. What you type in the terminals is never logged.

### Saving the log

`runtree --log-db runs.db tree.yaml` saves the events and the terminal output of the processes in an SQLite database.
Every runtree session is a new run in the same file, so it keeps the history of the runs. It is written in the
background (in batches, every 100 ms), and it can be read while runtree runs:

```
$ runtree --log-db runs.db --show-runs
RUN  STARTED              FINISHED             HOST  USER   EVENTS  OUTPUT   TREE
1    2026-10-17 23:45:44  2026-10-17 23:52:10  box   admin  31      12.4 KB  /home/admin/trees/upgrade.yaml
$ runtree --log-db runs.db --show-events
2026-10-17 23:45:44 INFO  scheduler started
2026-10-17 23:45:44 INFO  [n2] upgrade › host7: starting on 10.0.0.7:5000: apt-get -y upgrade
2026-10-17 23:47:12 WARN  [n2] upgrade › host7: exited with code 100 after 1m28s
...
$ runtree --log-db runs.db --show-output host7 | less -R
```

`--show-events` and `--show-output` show the last run, `--run ID` selects another one. `--show-output` takes the id of
the node (in brackets in the events), its title or its path, and prints the raw terminal output (with the escape
sequences, `less -R` or a terminal shows it). When the node was run more than once, it shows the last start, `--start
N` selects another one. The output is what the terminal showed, so it also has what the processes echoed of your
input (but not passwords, programs do not echo them).

The tables (the times are UTC, `datetime(time, 'localtime')` converts them):

- `runs`: `id`, `started`, `finished` (empty while runtree runs, or when it was killed), `tree_file`, `host`, `user`,
  `pid`, `version`, `dropped` (the number of events that were not saved, because the disk was too slow)
- `nodes`: the nodes of each run: `run_id`, `node_id`, `parent_id`, `type`, `label` (the title or the command), `path`
- `events`: `run_id`, `time`, `level`, `node_id`, `event`, `message`, and `attrs`: the other attributes as a JSON
  object (durations are in seconds)
- `output`: `run_id`, `node_id`, `start` (1, then 2 for a re-run...), `time`, `data`: chunks of the raw terminal
  output

For example:

```sql
-- the nodes that failed in the last run
SELECT n.path, e.time FROM events e JOIN nodes n USING (run_id, node_id)
WHERE e.run_id = (SELECT max(id) FROM runs) AND e.event = 'status' AND e.attrs ->> 'to' = 'Failed';

-- the slowest processes of all runs
SELECT e.run_id, n.path, e.attrs ->> 'elapsed' AS seconds FROM events e JOIN nodes n USING (run_id, node_id)
WHERE e.event = 'exited' ORDER BY seconds DESC LIMIT 10;
```

## Editor support

Name tree files `*.rt.yaml` (e.g. `upgrade_servers.rt.yaml`), so that editors can recognize them. runtree itself
accepts any file name, and includes refer to the files by their names.

`schema/runtree.schema.json` is a JSON Schema of tree files (`runtree --schema` prints the same). With the YAML
extension of VS Code (Red Hat), it gives completion, documentation on hover, and validation of property names and
values. Associate it with your tree files in the VS Code settings:

```json
"yaml.schemas": {
  "https://raw.githubusercontent.com/nagylzs/runtree/main/schema/runtree.schema.json": ["*.rt.yaml"]
}
```

or put this comment at the top of a tree file:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/nagylzs/runtree/main/schema/runtree.schema.json
```

The schema only checks the structure; `runtree --check` also checks includes, variables and the other semantics.

The YAML extension reports the `!raw` tag (see [raw strings](#raw-strings)) as unknown, unless it is listed in the
settings (the runtree extension does this):

```json
"yaml.customTags": ["!raw scalar"]
```

### Language server

`runtree-lsp` is a language server for tree files: editors get the errors and warnings of `runtree --check` as you
type (with the unsaved changes, and for the included files too), go to definition for `include` arguments, the
values of a `{variable}` on hover (in every node that the line belongs to, e.g. for each `for_vars` combination or
include, with where the value is set), and completion of `include` arguments (the objects of the file, and
`file|name` for the other runtree files). It
checks the YAML files of the workspace that have a top level `tree` object, the files that they include, and the open
`*.rt.yaml` files (a `*.rt.yaml` file that no tree includes is checked object by object, without the undefined variable
warnings, because the variables are set where the objects are included).

It does not need GTK, so it builds on Linux, Windows and macOS:

```bash
go install github.com/nagylzs/runtree/cmd/runtree-lsp@latest
```

(`scripts/build.py` builds it too.) The VS Code extension in `editors/vscode` starts it, does the schema association
for `*.rt.yaml` files, and it has a problem matcher for `runtree --check` tasks (see its README).


# Building on Windows

It is possible to build the `rtrunner` utility with a regular go compiler. But building the `runtree` (GUI) is not possible, because runtree uses the VTE terminal emulator, and it depends on syscalls that are not available on Windows.


# GTK 4 settings

The `runtree` utility is a GTK 4 application.

## Application id

The application id is `com.github.nagylzs.runtree.gui` defined in the `internal/gtkui` package.

## Theme

It was designed to work with dark themes. You can set the default theme by setting the environment variable GTK_THEME:

```bash
export GTK_THEME=Adwaita:dark
```

It also works with light themes, but some parts can still look dark.

If you have a HDPI screen, then you might want to scale:

```bash
GDK_SCALE=2
```

## Components

There are two main parts: the scheduler `runtree` and the runners. The scheduler decides what needs to be run and
where. A runner receives commands from the scheduler and executes them: allocates pseudo-terminals, keeps track of the
process states, etc. The scheduler has a GUI. Terminals opened by the runner can be displayed and interacted with in the
scheduler's GUI. One scheduler can use multiple runners at the same time, and one runner can run several commands in 
parallel. The GUI is used for displaying the structure of the tree, the current state of the processes, and also to
provide an interface where the user can intervene.

The `runner` property of the nodes chooses the runner:

- `local` (the default): the runner that is built into `runtree`. Nothing else needs to be started, and the processes
  run on the machine of `runtree`, as the user who started it.
- `ssh://[user@]host[:port][/path/to/rtrunner]`: for each process, `runtree` starts `ssh -T user@host rtrunner --stdio`,
  and the process runs on the host, in a terminal of `rtrunner`. Its `envs` and `cwd` apply on the host (the default
  `cwd` is the home directory of the user), the signals of the GUI reach it, and its exit code and CPU times are the
  ones of the process, not of `ssh`. A failed login is a start error of the node, with the message of `ssh`.
  - `ssh` uses `~/.ssh/config`, the keys and `ssh-agent`, but it cannot ask for passwords or passphrases (`BatchMode`),
    and the host key must be known already (connect with `ssh` once).
  - `rtrunner` must be installed on the host. It is a static binary, copy it from `dist/linux/amd64/`, built with
    `runtree` (an `rtrunner` with another protocol version is a start error). Without a path, it is started from the
    `PATH` of the user, which is short in an `ssh` command (e.g. `/usr/local/bin:/usr/bin`);
    `ssh://root@host/~/bin/rtrunner` starts `~/bin/rtrunner`.
- `host:port` or `tcp://host:port`: an `rtrunner` daemon, that listens on a TCP port (`rtrunner -l :5000`). The
  connection is neither authenticated nor encrypted: anyone who can connect to the port can run commands as the user
  of `rtrunner`, so use it on trusted networks only.

Without an `ssh://` runner, commands can also use `ssh` themselves on the local runner (e.g. `args_prefix: [ssh, -t,
"root@{host}"]`), but then `envs` and `cwd` apply to the local `ssh` process, the signals reach `ssh`, and a failed
login is exit code 255, like any other failure of the command.

A node connects to its runner in the background (the GUI shows "Running, connecting"), so a slow login does not hold up
the scheduler or the GUI, and the nodes of a `par` node connect at the same time.

The nodes that do not specify a runner (and their parents do not either) use the local runner, or the one given to
`runtree`: `runtree --runner testvm:5000 tree.yaml` runs them on another machine, without changing the tree file. The
nodes that must run on the machine of `runtree` (e.g. waiting for a host with `ping`) should say `runner: local`.

When `runtree` exits (the main window is closed, or it gets `SIGTERM`), it stops starting nodes, and terminates the
processes of the local runner that are still running: `SIGINT`, then `SIGTERM` after 10 seconds and `SIGKILL` after 10
more, and it waits until they exit. An `rtrunner` (also the one started by `ssh`) does the same when `runtree`
disconnects, but `runtree` does not wait for it, so those exits are not logged. If `runtree` is killed (`SIGKILL`), the
processes of the local runner get `SIGHUP` when their terminals are closed.

## Runtree and nodes

A `run tree` is defined by a simple YAML configuration file. Every `run tree` is built out of `nodes`. In the
configuration file, there must be a root node called `tree`. The `runtree` (scheduler) runs and displays that tree.
There can be different types of nodes in the tree:

* `run` simple run type nodes, they run a command in a terminal
* `seq` sequential type nodes, they go over sub-nodes, and execute them in a given order, sequentially
* `par` parallel type nodes, they execute their sub-nodes in parallel

There are other types that will be discussed later.

Each node has a state, which is one of:

* Waiting
* Frozen
* Running
* Paused
* Cancelled
* Failed
* Success

There are certain operations that can be performed on nodes, some of them change the state of the node:

* Freeze
* Thaw
* Start
* Cancel
* Signal
* Reset to Waiting

# Documentation

## General node properties

Every node can have the following general read-write properties. They can be specified in the configuration file.

- `type` - type of the node, a string that defines the type of the node. (`run`, `par` and `seq` will be described below)
- `id` - It must be an identifier-like string matching the regular expression `[a-zA-Z][a-zA-Z0-9_\-]*`. The id is used 
  to identify the node, mostly in the tree view of the GUI. When the id is not given, it is assigned a default generated
  value.
- `title` - A single line string, human-readable, displayed in the GUI
- `description` - Multi-line string, with the details of the node. Displayed in the GUI, when the node is selected.
- `cwd` - startup directory where the contained process(es) should be started. When not specified, or set to an empty 
  string, sub-nodes inherit the value of the `cwd` property from the parent node. When the inherited value is also
  empty, then it will be set to the current directory of the runner (of `runtree` for the local runner, or of the
  `rtrunner` process).
- `vars` - it can be a simple object containing name-value pairs. This object will be used in the subtree of the node
  for starting new processes, in particular for generating commands from the command templates (see below).
- `def_vars` - similar to `vars`, it defines variable values only if they are not already defined and inherited from the
  parent node. It is an error to put the same variable name in both vars and def_vars (in the same node)
- `inherit_vars`:
    - `true`: the node inherits all variables from its parent node. Inherited values can be overwritten with locally
      defined values. Default value is `true`. 
    - `false`: only locally defined values will be used. Please note, that if you set `inherit_vars` to `false`, then
      you cannot use `def_vars` (because that relies on inheritance) 
- `envs` - it can be a simple object containing name-value pairs. Values must be strings. This object will be used for
  starting new processes, in particular for setting environment variables.
- `system_envs`:
    - `true`: Use the environment of the runner (of `runtree` for the local runner, or of the `rtrunner` process) as
      a base environment for all subprocesses. Default value is `true`.
    - `false`: use an empty environment as a base for all subprocesses. Please note that in this case, you will have to
      define `PATH`, or specify every command with absolute filepath.
- `inherit_envs`:
    - `true`: the node inherits all environment variables from its parent node, recursively. Inherited values can be
      overwritten with locally defined values. Default value is `true`.
    - `false`: the node does not inherit environment variables from its parent
- `xlocks` - it is a list of strings, containing the names of resources that need to be exclusively locked while the
  node is active. See a detailed explanation below.
- `rlocks` - it is a list of strings, containing the names of resources that need to be locked while the node is active.
 These are reentrant locks. See a detailed explanation below.
- `provides` - it is a list of strings, containing the names of resources that the node provides (see below)
- `requires` - it is a list of strings, containing the names of resources that the node requires (see below)
- `max_proc` - maximum number of processes, for this subtree. -1 is the default, and it means infinite. (see below)
- `success_exit_codes` - a list of process exit codes (0-255) that mean success. When not specified, it is inherited
  from the parent node, and the default is `[0]`. The listed codes replace the default, so list 0 too if it should
  also mean success. It is useful for commands that are expected to fail, for example `ssh host reboot` exits with
  255 when the connection is closed by the rebooting host: `success_exit_codes: [0, 255]`. A process killed by a
  signal is always a failure.
- `status` - the initial status of the node. This property can be inherited from the parent node (when the tree is 
  loaded). The default status is `waiting`. Possible values are `waiting` and `frozen` (there are other statuses, 
  but they cannot be specified as initial statuses).
- `expanded` - boolean, defaults to `false`. When set to false, the GUI will not show children of the node.
- `expand_on_active` - boolean, defaults to `true`. When the node becomes active, it is expanded in the GUI.
- `collapse_on_finished` - boolean, defaults to `true`. When the node becomes finished, it is collapsed in the GUI.
- `args` - a list of strings, this provides arguments for executing the node. For `run` nodes, this is used
  to calculate the command line arguments that will be executed with `os.Exec`. The value of `args` cannot be inherited.
- `args_prefix` - a list of strings, this will be prepended to `args` to form the command line arguments. This can be 
  inherited from the parent, and it is useful when there are lots of sub-nodes with the same argument prefixes.
- `runner` - the runner that runs the processes of the node: `local` (the runner in `runtree`), an `rtrunner` started by
  `ssh` (`ssh://[user@]host[:port][/path/to/rtrunner]`), or the address of an `rtrunner` daemon (`host:port` or
  `tcp://host:port`), see [Components](#components). Variables can be used. When not
  specified, then it is inherited from the parent node. The root node uses `local`, or the `--runner` option of
  `runtree`.
- `on_error` - this determines what to do when the underlying process or a subnode fails. It is a string, or a list of
   two strings, containing the following values:
      - `failed`, `paused`, `success`: what should be the status of the node. The default is `failed`.
      - `none`, `skip`, `freeze`: what operation should be performed on the not-yet-started siblings. The default is
        `none` (which means do nothing with the siblings)
   You can only specify one status and one operation at most. When `on_error` is not specified, then it is inherited
   from the parent node: from its `children_on_error` property if it has one, otherwise from its `on_error`. The root
   node defaults to `failed` and `none`.
- `children_on_error` - the `on_error` value that is inherited by the sub-nodes (that do not specify `on_error`), with
   the same syntax as `on_error`. When not specified, the sub-nodes inherit the `on_error` of the node. It is useful
   when the node itself should behave differently from its sub-nodes. For example, a seq node that pauses when one of
   its steps fails, while the steps (and everything below them) simply fail:

  ```yaml
  - type: seq
    title: "Upgrade OS"
    on_error: paused
    children_on_error: failed
    nodes: [ ... ]
  ```
- `ifeq` - conditionally includes or excludes the node from the tree. The node will only be included in the tree if
  all given variables have the given values
- `ifneq` - similar to `ifeq` but it works in reverse: it includes the node if all given variables are different from
  the given value

Nodes also have the following read-only properties. They cannot be configured:

- `active`
- `finished`
- `has_error`

These are not stored properties, but a function of the state. For details, see the list of states below.

## Syntax

Nodes appear in the configuration file as simple YAML objects, for example:

```yaml
tree:
  # General properties
  title: "My first rt"
  cwd: "~/my_project"
  envs:
    DEBUG: "1"
    HALT_ON_ERROR: "0"
  rlocks: [ "build-server" ]
  type: "run"
  # type-specific properties
  args: [ "make", "-j", "3" ]
```

The order of properties is not important, but usually you will want to put general properties at the top, and 
type-specific properties at the bottom.

Tree files are read as YAML 1.2: `yes`, `no`, `on` and `off` are strings (e.g. in `args`), but boolean properties
accept them too. Anchors, aliases and merge keys (`<<: *anchor`) can be used, and a key must not be repeated in an
object.

## Maximum number of processes

If the root node has no `max_proc` specified, then it defaults to -1. The value of -1 means that the number of processes
is not restricted. If the number of already running processes reaches or exceeds `max_proc` for a given subtree, then 
the scheduler won't start new processes in that subtree. It will wait until the number of running processes goes under 
the maximum.

Please note that you can still manually start new processes, `max_proc` only affects the scheduler.

### Variables and default variables

Variables are name-value pairs, defined with objects after the `vars` property. When a `vars` block is encountered, new
variable values are evaluated first. Variable assignment takes place AFTER the whole `vars` block has been parsed and
evaluated. This makes it possible to overwrite variable values using expressions containing other (or the same) variable 
values.

Default variables can be given after `def_vars`. You cannot set the value of the same variable in `vars` and `def_vars`
in the same node. The variables defined `def_vars` will only be used if they are not already defined in the parent node. 
Using `def_vars` with `inherit_vars=false` is an error.

Variables for a node are calculated as follows:

1. Parent variables are calculated before children.
2. If `inherit_vars` is `true`, then the variables of the parent are the base. Otherwise, the base is empty.
3. The `for_vars` variables of the parent (the combination that the node was generated for) are added to the base.
4. `def_vars` are added for the variables that are not in the base. Their values can use the variables of the base
   and the other `def_vars` (see below).
5. `vars` are added, they override the variables of the base. Their values can use the base and the other variables
   of the same block (see below).
6. The result is then used as the calculated variables map for the node.

Variable values can be substituted for various properties:

- `vars` (create variable VALUES from other variables, see the fifth point above)
- `def_vars` (create default variable VALUES from other variables, see the fifth point above)
- `title`
- `description`
- `args`
- `args_prefix`
- `cwd`
- `envs`
- `runner`
- `rlocks`
- `xlocks`
- `provides`
- `requires`

Inside a given node (YAML object), the evaluation of `vars` and `def_vars` always happen BEFORE the evaluation of other
properties.

Variable substitution is noted with `{VARIABLE_NAME}` syntax inside strings. The substitution is done in a single pass:
the substituted values are not evaluated again. Undefined variables are left as they are (`{VARIABLE_NAME}`), and they
are reported as warnings (see [Checking a tree file](#checking-a-tree-file)). Only string variables can be substituted,
it is an error to use a list variable (or a number) in a string.

The strings in list variables are calculated in the node where the variable is defined, like the other values.

#### Raw strings

A string that is tagged `!raw` is used as it is, without variable substitution (and without undefined variable
warnings). It is useful for commands that contain curly braces, for example:

```yaml
args: ["bash", "-c", !raw "jq '{name: .user}' data.json"]
```

The tag applies to the whole string. It can be used for any string value (e.g. in `args`, `title`, `envs`, `vars`, or
in a list variable), but not for object keys, lists or objects. Other tags are errors.

TODO: maybe we could use https://expr-lang.org/docs/language-definition instead?

Simple example for a variable:

```yaml
tree:
  type: "seq"
  vars:
    target: "test"
  nodes:
    - type: "run"
      # This will "make test"
      args: ["make", "{target}"]
```

Example for disabled variable inheritance:

```yaml
my_parent_node:
  type: "seq"
  vars:
    target: "test"
  nodes:
    - type: "run"
      inherit_vars: false
      # "target" variable is undefined (not inherited) here
      args: ["make", "{target}"]
```

The values in a `vars` block can use the other variables of the same block, and the inherited variables. A variable
of the block overrides the inherited one with the same name, in the other values of the block too. The order of the
variables in the block does not matter, and they can refer to each other through any number of steps (but not in a
cycle, that is an error). In its own value, a variable means its inherited value, so it can be extended.

The values in a `def_vars` block can use the inherited variables and the other defaults of the same block, with their
final values: in the example below, `url` uses the inherited `host` when there is one, otherwise the default. They
cannot use the `vars` of the same node: `def_vars` stand in for the values that are not given to the node (e.g. where
it is included), and those cannot depend on the variables of the node itself. When it is needed, put the node into a
`seq` node that has those `vars`.

```yaml
deploy:
  def_vars:
    host: "localhost"
    port: "8080"
    url: "http://{host}:{port}"
  args: ["curl", "{url}"]
```

Example:

```yaml
outer_node:
  vars:
    target: "build"
    path: "/usr/bin"
  type: "seq"
  nodes:
    - type: "run"
      vars:
        # "make test": {target} is the value in this block, it overrides the inherited "build"
        command: "make {target}"
        target: "test"
        # "/usr/bin:/opt/tools/bin": in its own value, {path} is the inherited value
        path: "{path}:/opt/tools/bin"
      # This will run "make test" in a shell, with PATH=/usr/bin:/opt/tools/bin
      envs:
        PATH: "{path}"
      args: ["bash", "-c", "{command}"]
```

TODO: strict mode: an option to make the undefined variable warnings errors

## Environment variables

Environment variables for new processes are calculated this way:

1. If `system_envs` is `true`, then system envs (inherited from the parent process of `runtree`) are taken as a base.
   Otherwise, the base environment will be empty.
2. `inherit_envs` is `false`, then the base environment updated with the locally defined `envs`, and the resulting env
   is used to start a new process.
3. Otherwise, if `inherit_envs` is `true`, then the environment is first calculated for the parent, then it is updated
   with the locally defined `envs`, and the resulting envs are used to start a new process.

For example, if you want to start with a clean empty environment and define every value locally in the run node:

```yaml
my_parent_node:
  type: "seq"
  envs:
    DEBUG: "true"
  nodes:
    - id: my_run_node
      type: "run"
      system_envs: false
      inherit_envs: false
      envs:
        PATH: "/bin;/usr/bin"
      # Here, "DEBUG" env will be undefined!
      args: ["make"]
```

Another example: extend the system environment with your own set of values but do not inherit values from the parent
node:

```yaml
- id: my_run_node
  type: "run"
  system_envs: true
  inherit_envs: false
  envs:
    DEBUG: "true"
  # will look for "make" on system PATH
  args: ["make"]
```

You can use normal variables to create new environment variables, as shown below. Inside an object (YAML block), `vars`
are always evaluated BEFORE `envs`, even if `envs` appear sooner in the file.

```yaml
- id: my_run_node
  type: "run"
  vars:
    host: "server01"
    user: "user01"
  envs:
    PGHOST: "{host}"
    PGUSER: "{user}"
    PGDATABASE: "{user}"
  args: ["psql", "-i", "input.sql"]
```

## Locking resources

There can be certain resources that can only be used by one process (or group of processes) at a time. A good example
would be a software build service that can only build a single project at a time. You can name these resources, and 
list them in the `xlocks` and `rlocks` properties of the node. When the node is started by the
scheduler (goes from an inactive state into an `active` state), then it acquires these locks. When it becomes 
`inactive`, then it releases them. The difference between exclusive and re-entrant locks is that exclusive locks can 
only be acquired by a single node, while re-entrant locks can first be acquired by a single node, and also acquired
by any of its sub-nodes, possibly multiple times. Thus, re-entrant locks are held by a subtree and are initially
acquired by the top node of that subtree.

The resource names are shared between `xlocks` and `rlocks`. So if a resource is locked by a node that lists it in its
`rlocks` property, then it cannot be locked by another node that lists it after `xlocks` property. It is an error to
list the same resource in `rlocks` and `xlocks` within the same node.

Regarding a single node, locking is "all or nothing". If a node lists multiple locks, then they are either all acquired
at once, or none of them are acquired. This operation is also atomic: the node changes state to `active` (e.g., running)
exactly when its locks are acquired. It is possible to acquire a lock in a parent, and then acquire additional locks in 
a child. It is possible to create a `run tree` with deadlocks. Since the presence of a deadlock may depend on 
semantics, it is the user's responsibility to avoid them.

Lock names can be constructed using variables. For example:

```yaml
vars:
  host: "server01"
  branch: "stable"
# Locks "server01-stable"
rlocks: [ "{host}-{branch}" ]
```

The `xlocks` property cannot be inherited from the parent. It makes no sense (only one node can hold an exclusive
lock). The `rlocks` property cannot be inherited from the parent either. Under normal circumstances, when a node with
an rlock becomes active, then the topmost parent with the same rlock will also become active, and that "root" will
hold the r-lock. You can, however, manually start a sub-node of an already finished node, but that is manual 
intervention.

TODO: add xlock-deadlock detector to the scheduler.

## Dependency-based synchronization: providing and requiring resources

The `provides` and `requires` properties can be used to implement dependency-based synchronization between processes.
A good example would be a binary file that is provided (built) by a node (build command), and is required (used) by 
another node (deploy command).

Any node can list resource names (simple strings) after the `provides` and `requires` properties. 
The `runtree` utility keeps track a list of resources that are available or **provided**. Initially (when the tree is 
loaded and the root node is started), this list is empty. When a node reaches `success` state, then the resources listed 
after its `provides` property are added to the list of available/provided resources. Resources are only removed from
this list by the [reset to waiting](#reset-to-waiting) operation. The scheduler will not start a node until all required
resources are provided by other (success) nodes. It is possible to create deadlocks with dependency synchronization, it is the user's responsibility to avoid that.

The `provides` property cannot be inherited from parent nodes, it would make no sense; because under normal 
circumstances, the parent cannot be finished before the child, and so the child will provide the resource before 
the parent.

Dependency-based resource names are subject to variable substitution. For example:

```yaml
vars:
  branch: "stable"
# Provides "binary-stable"
provides: [ "binary-{branch}" ]
```

Provided and required resource names are independent of lock names.

TODO: add dependency-deadlock detector to the scheduler.

## Node statuses

Node state (or status) is a property of every node that is particularly important. Any node is assigned a single status
at any time. The initial default status is `waiting`. This can be changed to `frozen` in the configuration file. It 
marks a point in the execution that requires user interaction.

| Status       | Description                      | Is active | Is finished | Has error | Requires manual input   |
|--------------|----------------------------------|-----------|-------------|-----------|-------------------------|
| Waiting      | Want to start, not started yet   | No        | No          | No        | No                      |
| Frozen       | Want to start later (manual)     | No        | No          | No        | Yes                     |
| Running      | Started and not finished.        | Yes       | No          | No        | No                      |
| Paused       | Almost finished (manual)         | No (1)    | No          | No        | Yes                     |
| Success      | Finished normally.               | No        | Yes         | No (2)    | No                      |
| Failed       | Finished with errors.            | No        | Yes         | Yes       | No                      |
| Cancelled    | Do not want to start.            | No        | Yes         | No        | No                      |

- (1) The `paused` status is technically not active (no underlying processes are running), but setting the final status
  is up to the user, so it is not finished because it is waiting for the user to decide the final status.
- (2) Technically, the Success status may have an error message that can be displayed. It can happen if `on_error`
  was set to `success`. Then the scheduler treats the node as if it had no error, but the error message is preserved 
  and can be displayed.

Meaning of derived state properties:

- `active` - An `active` node means that the node has created a process, and it has not exited yet, or it can mean that
  at least one of its sub-nodes is active.
- `finished` - A finished node has a state that will not be changed by the scheduler. This property tells that the node
  is considered "processed and final" by the scheduler, and the scheduler thinks that there is nothing to do with it.
  The user can still manually change states, though. The whole tree is considered "finished" when the root node becomes
  finished.
- `has_error` - usually indicates that a process exited with a non-zero error code (Failed), or it is exiting or exited 
  after receiving a signal, or that one of its subnodes has an error. More generally, `has_error` means that something
  unexpected happened.
- The last "manual input required" column indicates that the status will not be changed by the scheduler; it can only be
  changed manually by the user, using the GUI. There are two such statuses: `frozen` is **before** the node is started, 
  and `paused` is **after** the node has started. You can read more about them below.

Errors and success status

Normally, when a process exists with a non-zero exit code, then the corresponding node goes into `failed` state. 
Similarly, if a subnode becomes failed, then the parent node goes into the failed state. However, there are some cases 
where we do not care about the exit code, and want to treat the job as if it was successful, even if the process has 
failed. This can be achieved by setting the `on_error` property to `success`. In those cases, a node can reach 
`success` state, with an error message. Technically, the error may be available and can be displayed, but the node 
goes into `success` state and the scheduler will treat the node as it had no error. So the `has_error` property means 
that we think that nothing unexpected happened. It can mean that there was no error, or it can mean that there was an 
error which was ignored.

## Node operations

### Operations on single nodes

There are certain operations defined **on single nodes**:

* `freeze` - set `frozen` status, can only be performed by the user in `waiting` status, this is a manual operation,
  the scheduler will never perform it.
* `thaw` - set `waiting` status, can only be performed by the user in `frozen` status, this is a manual operation,
  the scheduler will never perform it.
* `run` - the scheduler can only perform this in `waiting` state for run-type nodes, and only when all conditions are 
   met (for example, locks can be locked and required resources are provided). The user can manually perform this 
   in inactive states on run-type nodes.
* `fail` - the scheduler can perform this in `running` state, when the node fails (e.g., the corresponding process fails,
   or one of its sub-nodes fails). It can also be set manually in `paused` state.
* `success` - the user can perform it manually in `paused` state, to treat the node as successful (it provides its
   resources, and the scheduler continues).

  When `fail` or `success` is performed on a paused `seq`/`par` node, then it sets the status of that node, but it does
  not change its sub-nodes (their not yet started sub-nodes will not be started). This manual status lasts until an
  operation is performed on the node, or on any node below it; then its status is calculated from its sub-nodes again.
* `signal` - can only be performed in `running` state, it sends a signal (selected by the user) to the process
* `reset to waiting` - set `waiting` status, so that the scheduler starts the node again (when its requirements are
   met). The user can perform it on run-type nodes in `frozen`, `paused` and finished (`success`, `failed`,
   `skipped`) states, and on `seq`/`par` nodes in `frozen`, `paused` and `skipped` states. Running nodes cannot be
   reset. It may unprovide resources, see [Reset to waiting](#reset-to-waiting).

### Freeze and thaw

The `freeze` and `thaw` operations are performed by the user manually (on the user interface). `freeze` can be performed
in `waiting` state, `thaw` can be performed in `frozen` state. In the GUI, this can be a recursive operation: when you 
activate the `freeze all` or `thaw all` action on a node, then it will be performed on the node and all of its subnodes 
that are in the appropriate state.

A node in the `frozen` state is very similar to a `waiting` node, except that the scheduler will never consider 
running it. By freezing the node, you can instruct the scheduler not to start the node yet, but also telling that it
is not finished yet (will change state later). 

The `freeze` and `thaw` operations are non-blocking (e.g., they can always be performed in a very short amount of time),
but freezing a node may block other operations (through dependencies or locks and other ways).

It is possible to define initially `frozen` nodes in the `run tree`:

```yaml
- id: my_run_node
  type: "run"
  state: "frozen"
  args: ["make", "build"]
```

This creates a breakpoint in the tree. User interaction is required to "run and go beyond" an initially frozen node.

### Run

The `run` operation is what the node should normally do when scheduled. The run operation may take a long time. Starting
the run operation is also referred to as "starting the node". When the node is successfully started, then it goes into 
`running` state.

The user can run a node manually too, also a finished one again (`Re-run` in the GUI). A finished node does not
provide its resources while it runs again, and neither do its successful parents (they become running): these
resources are **unprovided** (unless other nodes provide them too), so the nodes that require them wait until the node
finishes again. The GUI shows the same dialog as for [reset to waiting](#reset-to-waiting), where you can keep some
of them provided. A re-run that cannot be started (e.g. the runner is not available) makes the node failed, and
it unprovides the resources too.

### Reset to waiting

The `reset to waiting` operation is used to go over some steps again, for example after the source code was fixed
because a test failed. Performed recursively (`ResetToWaiting all` in the GUI), it resets every node of the subtree that
can be reset, so the whole subtree runs again. Running nodes are not reset.

The resources provided by the nodes that are reset (and by their parents that become unfinished because of the reset)
are **unprovided**, unless they are also provided by other nodes. This makes the nodes that require them wait until
they are provided again. For example, if `build` provides `binary`, `deploy` requires `binary` and provides `deployed`,
and `run` requires `deployed`, then resetting all of them unprovides `binary` and `deployed`, so `deploy` waits for the
new `build`, and `run` waits for the new `deploy` (even if they run under a `par` node).

When the reset would unprovide resources, the GUI shows a dialog where you can choose to unprovide all of them
(default), or select the resources that should be unprovided. A resource that is not unprovided stays available,
so the nodes that require it can start right away, without waiting for the node that provided it.

Resetting a node also clears the error of its previous start (e.g., when the runner could not be reached).

### Cancel

The `cancel` operation works on node or a subtree. This will permanently set `cancelled` state of all `waiting` 
and `frozen` nodes in a subtree of nodes. This operation can be performed manually on any node.

The `cancelled` state means that the node was not started, and it does not ever need to be started; and it is not 
supposed to change state (it has a final state). It is different from `frozen`, which means that we want to start it 
or change its state some time in the future, but not right now.

### Signal

This will send a signal (`SIGINT`, `SIGTERM` and `SIGKILL`, etc.) to the underlying process of the
node. The signal operation is only valid for run type nodes, and only when it has an underling process.

### Recursive operations on subtrees

On the user interface, recursive operations can be performed on subtrees. It just means that the operation will be 
performed recursively on all nodes of the selected subtree, if applicable. For example, `signal` will be performed on 
all `running` nodes in the selected subtree. Any node in the subtree that does not 
implement or cannot perform the given operation in its current state, the (recursive) operation does nothing.

TODO: how to list possible signals for a subtree that has different runners?

### Blocking and unblocking

Every node has a property called `blocked`. The scheduler automatically recalculates the value of this property as given
below:

1. Locks are calculated from `rlocks` and `xlocks`. If the required locks cannot be acquired, then the node is blocked.
2. Dependencies are calculated from `requires`. If the required resources are not provided, then the node is blocked.
3. Otherwise, the node is not blocked.

The above algorithm is used to change the `blocked` property of each node continuously. In particular, this happens
right before the node is started. If the node is `blocked`, then the scheduler won't try to start the node, but wait
until it becomes available (not blocked). Note: only `waiting` nodes are considered. Although `frozen` nodes may not be 
blocked, but they won't be started (for a different reason). Also, when `max_proc` is reached, then the scheduler won't
start new nodes (even if they are not blocked and not frozen).

# Node types

## `run` - Run command node

The purpose of a command node is to execute a single command (start and run a new process). This is the only type that
can directly be started by launching an underlying process (either manually, or automatically by the scheduler).

### Properties

* `type` - must be `run`
* `args` - all "run command" nodes must have this property. It is
  a list of strings (they will be escaped to arguments even if they contain special characters). The
  actual arguments will be generated from this value, using variable substitution.
* `runner` - the runner that starts the process: `local` (the default), `ssh://[user@]host`, or the `host:port` of an
  `rtrunner`

### Syntax

Command nodes are the most common. To make configuration files more compact, they can be defined with just a list of
strings (without giving any properties). E.g., when a list is provided instead of an object (node), then it is treated
as if it was a run node.

Instead of this:

```yaml
tree:
  type: "seq"
  nodes:
    - type: "run"
      args: ["chown", "-R", "root:", "debug"]
    - type: "run"
      args: [ "rm, "-fr", "tmp" ]
```

You can use this:

```yaml
tree:
  type: "seq"
  nodes:
    - ["chown", "-R", "root:", "debug"]
    - [ "rm, "-fr", "tmp" ]
```

Or the shortest form:

```yaml
tree:
  seq: [ ["chown", "-R", "root:", "debug"], [ "rm, "-fr", "tmp" ] ]
```

### Operations

#### Run

When a command node is started, then it creates a new process.

1. Can be performed in `waiting` state only, and only if starting the node will not violate the `max_proc` constraint.
2. If the node is `blocked`, then the node is not started, and does not change state. The scheduler will wait until 
   it becomes unblocked.
3. Variables are calculated from the `vars` and `inherit_vars` properties. (See above.)
4. Environment variables are calculated from the `envs`, `system_envs` and `inherit_envs` properties. (See above.)
5. The command to be run is calculated from the `args` property, by substituting values from the variables calculated in
   the previous steps. Also, the working directory of the process is calculated from the `cwd` property, by substituting
   variables into its value.
6. The new process is started, the state of the node changes to `running`. Configured locks are acquired in the same
   atomic step.
7. Then the scheduler waits until the process is exited. (This is the only step where `run` waits indefinitely.)
8. After the process exits, the state of the node changes to `success`, if the process exited with return code zero,
   `sucess` if there was an error, but `on_error` was set to `success`, and `failed` otherwise.
9. When the node is finished, then all locks acquired in step 6 are released. This step may unblock other nodes.
10. If the node reaches `success` state, then the resources listed after `provides` are **provided** to other nodes. 
    This step may unblock other nodes.

#### Cancel

This operation is manual, scheduler will never do it.

1. Can be performed in `waiting` and `frozen` states only.
2. Set `cancelled` (final) state, the node becomes `finished`.

### Signal

This operation is manual, scheduler will not do it. The user needs to select a signal to be sent.

1. The underlying process must already be running.
2. It sends the selected signal to the process.

## `seq` and `par` - sequential and parallel run nodes

They contain lists of subnodes that are scheduled sequentially or in parallel, by the scheduler.

A `seq` node contains a list of sub-nodes. They are started sequentially by the scheduler, in the given order. 

* In a sequential node, the scheduler will not start a sub-node if there is another active sub-node. 
* As long as the scheduler is starting the subnodes, at most a single sub-node will be active in a `seq` node, and each
  subnode is started once.
* However, it is possible to manually start any subnode.

A `par` node is very similar, the only difference is that the subnodes are started in parallel by the scheduler. Several
things can prevent the scheduler from starting a subnode: not provided requirement, a lock that cannot be acquired,
maximum number of processes reached, etc. The scheduler will try to start each node once, as soon as it is possible.
If it is possible, then the scheduler will start all of them at once.

For both types, the effective `max_proc` value may limit the number of processes, and it may prevent the scheduler to
start new sub-nodes (waited for).

### Properties

- `type` - must be `seq` or `par`
- `nodes` - the value of this property must be a list of nodes.
- `on_error`, `children_on_error` - see [General node properties](#general-node-properties).
- `for` - create a simple for loop to go over a set of properties. See details below.

TODO: default value of `on_error` should be determined by a command line switch! E.g., in interactive mode it should
be `pause`, in unattended mode it should be `cancel`.

To make `seq` and `par` nodes more compact, it is allowed to use a single `seq` or `par` property instead of `type`
and `nodes` specifications. For example, instead of this:

```yaml
tree:
  on_error: "freeze"
  type: "seq"
  nodes:
    - type: "run"
      args: ["make", "build"]
    - type: "run"
      args: [ "make", "deploy" ]
```

You can use this:

```yaml
tree:
  on_error: "freeze"
  seq:
    - type: "run"
      args: ["make", "build"]
    - type: "run"
      args: [ "make", "deploy" ]
```

The shortest form gives the run nodes as lists of arguments:

```yaml
tree:
  on_error: "freeze"
  seq: [ [ "make", "build" ], [ "make", "deploy" ] ]
```

### Activating and inactivating seq/par nodes, and their effect on locks

A seq or par node first becomes active (running) when at least one of its subnodes becomes active. It stays in active
state until **all** of its subnodes become finished. It is important to understand, that a seq or par node can stay
active (running), even if it has no running subnodes. For example, some of its subnodes can be finished (success, failed
or cancelled), other subnodes can be waiting or frozen for various reasons (not yet started by the scheduler,
blocked etc.) The seq/par node acquires rlocks/xlocks when it becomes active (running), and it will not release them
until all subnodes become finished.

This is intentional: if you place a lock in a par/seq node, then it will first acquire the lock when the first subnode
becomes active, and will hold it until the last subnode becomes finished. This allows you to group multiple processes
inside an atomic lock operation (for a single lock, or for a set of locks).

### Final state of a seq/par node

The seq/par node becomes `finished` when all of its sub-nodes become `finished`. The final state depends on the states
of its (`finished`) sub-nodes, and the value of its `on_error` property:

TODO: an algorithmic description would be better! What happens to waiting sub-nodes when a sub-node fails?

1. When all sub-nodes are finished without error, final state becomes `success`.
2. When all sub-nodes are finished with at least one error, then:
    1. If `on_error` is `fail` (the default) then the final state becomes `failed`. (This is the default.)
    2. otherwise (`on_error` is `force_success`), final state becomes `success`.

### Run operation of seq node

- The seq node runs its sub-nodes sequentially, in the given order.
- It will wait and do nothing if the seq node itself is `paused` (waited for).
- It will not start the next sub-node if the next sub-node is `paused` (waited for).
- It will not start `blocked` nodes (waited for, may cause deadlock).
- It will only run the next contained `waiting` sub-node after the preceding sub-node became `finished`.
- State of the seq node changes to `running` when the first sub-node is successfully started. Locks are acquired in the
  same atomic step.
- The seq node becomes `finished` (reaches final state) when all of its sub-nodes are `finished`, or when one of them
  fails (see below). Locks are released in the same atomic step.
- When a sub-node fails (becomes `finished` with error), then the seq node does not start its next sub-nodes. It gets
  the status given by its `on_error` property right away:
    - `failed` (the default): the seq node fails.
    - `paused`: the user decides what to do. For example, fix the problem and reset the failed sub-node to waiting,
      or set the seq node to `success` or `failed` manually.
    - `success`: the seq node succeeds (without running its next sub-nodes).
- A sub-node that is allowed to fail should have `on_error: success`, then it counts as successful, and the seq node
  continues with the next sub-node.

### Run operation of par node

- The par node runs its sub-nodes in parallel.
- It will wait and do nothing if the par node itself is `paused` (waited for).
- It will not start a sub-node if the sub-node is `paused` (waited for).
- It will not start `blocked` nodes (waited for, may cause deadlock).
- If the `max_proc` limit is not reached, and all required locks are can be acquired, then it can start all or some of
  its sub-nodes in parallel.
- State of the par node changes to `running` when the first sub-node is successfully started. Locks are acquired in the
  same atomic step.
- The par node becomes `finished` (reaches final state) when all of its sub-nodes are `finished`. Locks are released in
  the same atomic step.
- When a sub-node fails, the par node keeps running its other sub-nodes (they are usually independent), and it may
  also start its not yet started sub-nodes. It gets the status given by its `on_error` property (`failed` by default,
  `paused` or `success`) when all of its sub-nodes are finished. While it is still running, the GUI displays the
  number of failed run nodes below it (e.g. `Running (2), 1 failed`).
- To stop starting new sub-nodes after a failure, use the sibling operation of the sub-nodes' `on_error`: `freeze`
  freezes the not yet started siblings of the failed sub-node (they can be thawed later to continue), `skip` skips
  them. `children_on_error` sets it for all sub-nodes at once.

Example: a rolling upgrade, one server at a time, that stops at the first failed server. The remaining servers are
frozen, and the par node stays `running` until they are thawed (to continue the upgrade) or skipped (to finish it):

```yaml
- type: par
  title: "Upgrade servers"
  max_proc: 1
  children_on_error: [failed, freeze]
  nodes:
    - type: run
      title: "server1"
      args: ["ssh", "root@server1", "apt-get -y dist-upgrade"]
    - type: run
      title: "server2"
      args: ["ssh", "root@server2", "apt-get -y dist-upgrade"]
    - type: run
      title: "server3"
      args: ["ssh", "root@server3", "apt-get -y dist-upgrade"]
```

### For loops

Note: for loops logically belong to properties, and should be described before operations. It is described here because
it is easier to understand how it works if you already know how a parallel node runs.

Instead of specifying a single set of values for properties, you can use the `for` property to loop over a list of
property sets. The value of the `for` property must be a list of objects. These objects can contain the following
properties:

- `envs`
- `vars`
- `cwd`
- `rlocks`
- `xlocks`
- `provides`
- `requires`

When the `for` property is given, then the subtree defined by the `nodes` property will be repeated for every object
that is listed below the `for` property. If a property is listed as a static value and also as a for loop value, then
the for loop value overwrites it in its own cycle.

Here is a simple example:

```yaml
tree:
  for:
    - vars:
        branch: "dev"
    - vars:
        branch: "uat"
    - vars:
        branch: "stable"
  seq:
    - "make build {branch}"
    - "make test {branch}"
```

This will generate 3*2=6 nodes and execute them sequentially. The order will be:

- make build dev
- make test dev
- make build uat
- make test uat
- make build stable
- make test stable

In a more difficult the example below, there is a single seq sub-node defined in `nodes`, and there are three objects
listed after the `for` property. It will actually generate 3*1=3 nodes for parallel execution. Notice how the `host`
variable is used in the `rlocks` property. It creates re-entrant locks whose name depends on a variable that will be
different for every loop cycle.

```yaml
tree:
  # Variable values that are common for all loop cycles
  vars:
    user: "app_user"
    project: "project01"
  # Variables, environments and other properties that are different for each loop cycle
  for:
    - vars:
        host: "dev.server.com"
        branch: "dev"
      envs:
        DEBUGLEVEL: "2"
    - vars:
        host: "dev.server.com"
        branch: "uat"
      envs:
        DEBUGLEVEL: "1"
    - vars:
        host: "server03"
        branch: "prod"
  # run nodes in parallel
  par:
    # There is a single sub-node that is a sequence of commands
    - type: "seq"
      cwd: "/root/bin"
      rlocks: [ "{host}" ]
      nodes:
        # Build project
        - type: "run"
          rlocks: [ "build_server" ]
          args: ["./build_any", "-b", "{branch}", "-p", "{project}"]
        # Upload compiled code
        - "./upload_code -p {project}"
        # Rolling deploy on the remote server
        - "ssh -t {user}@{host} ./rolling_deploy"
```

The scheduler will try to start the generated 3 nodes in parallel. It can only start the first one at the beginning,
because all of them start with a build command that requires the same resource `build_server`. So the order of execution
will be:

1. start `build_any dev`
2. wait until `build_any dev` is finished (because all possible waiting command nodes require the same `build_server`)
3. then start `upload + rolling_deploy dev` sequence in parallel with `build_any uat`
4. wait until `build_any uat` is finished (because `build_any stable` requires the same `build_server`)
5. then start the following in parallel:
    1. `upload + rolling_deploy uat` sequence, but this will be serialized with step 3, because they use the same
       lock `{host}="dev.server.com"`
    2. `build_any stable` (because `build_server` can be acquired again)
6. wait until `build_any stable` is finished (sequence)
7. then start `upload + rolling_deploy` stable sequence
8. wait until all `upload` and `rolling_deploy` commands are finished
9. exit (run tree is finished)

The upload + rolling deploy commands started at step 3 may be finished before step 5, or only at the end of step 8. We
don't really care (and the actual finish times of sub-nodes can depend on factors that we may not be able to predict,
for example others might be stressing some servers with heavy loads).

Notice that the order of execution is synchronized and determined by locks. The `run tree` in this example is structured
by the concepts that are meaningful for the goals that we want to achieve: "go over projects" and "go over build and
deploy steps". By structuring the tree around these concepts, it becomes easy to overview and superwise the tree. The
structure of the tree does not show or represent the order of execution. If the run tree specification is correct,
then `runtree` will run everything in parallel, as much as possible. When an error occurs, the user interface will
show what part of the tree is failed, why and when.

#### Cancel, Interrupt, Terminate, Kill

These operations are not defined and cannot be performed on par and seq nodes directly (but they can be performed
recursively on their sub-nodes).

## `for_vars`

The `for_vars` construct can be used to repeat sub-nodes for a set of variable value combinations. The syntax is:

```yaml
for_vars:
  variable1: [string1, string2, string3]
  variable2: [string1, string2]
  variable3: reference1
```

For example:

```yaml
tree:
  vars:
    targets: ["dev", "uat"]
  for_vars:
      server: ["server01", "server02", "server03"]
      target: targets
  par:
    - args: ["ssh", "root@{server}", "make", "build", "{target}"]
```

This will iterate over the cartesian product of the possible variable values, and generate nodes for all of them,
in the order the variables are written: the first variable changes the slowest, like nested loops. The above example
is equivalent to:

```yaml
tree:
  par:
    - args: ["ssh", "root@server01", "make", "build", "dev"]
    - args: ["ssh", "root@server01", "make", "build", "uat"]
    - args: ["ssh", "root@server02", "make", "build", "dev"]
    - args: ["ssh", "root@server02", "make", "build", "uat"]
    - args: ["ssh", "root@server03", "make", "build", "dev"]
    - args: ["ssh", "root@server03", "make", "build", "uat"]
```

For variable value lists, you can only use a list of strings, or the name of a variable that is a list of strings.
The strings can contain the variables of the node (e.g. `server: ["{env}-01", "{env}-02"]`). When a list is empty,
there are no combinations, and the node has no sub-nodes.

The `for_vars` variables are set for the sub-nodes like inherited variables: the `vars` of a sub-node override them,
and its `def_vars` are not used for them. So an included object can have `def_vars` for the case when it is included
without `for_vars`.

It is important to understand that for_vars has na effect on the sub-nodes only. It is not possible to use it on a 
run node, because it has no sub-nodes.

## `include`

Instead of `nodes` it is possible to use `include`. This keyword loads another subtree from the same yaml file.

The argument of `include` can be a string or a list of strings. These strings will be interpreted as top level object 
names, and those top level objects will be parsed into new sub-nodes.

Example:

```yaml
tree:
  status: "frozen"
  type: par
  for_vars:
    server: ["server01", "server02"]
    target: ["dev", "uat"]
  include: ["make_and_deploy", "health_check"]

make_and_deploy:
  title: "build and deploy on {server}/{target}"
  seq:
    - args: ["ssh", "root@{server}", "make", "build", "{target}"]
    - args: ["ssh", "root@{server}", "make", "deploy", "{target}"]

health_check:
  title: "health check {target} on {server}"
  args: ["ssh", "root@{server}", "health_check", "{target}"]
```

It is possible to define a `run tree` with circular references, but it results in a compilation error (`runtree` will
throw an error and refuse to start the tree).

You can also load items from other YAML files, containing runtree objects. The file name is relative to the current
file, and the file name and the top-level object name is separated by pipeline `|` character.

```yaml
tree:
  status: "frozen"
  type: par
  for_vars:
    server: ["server01", "server02"]
    target: ["dev", "uat"]
  include: "build_tools.yaml|make_and_deploy"
```

## `load` nodes

The purpose of a `load` node is abstract code re-use. A `load` node can load tree definitions from another yml config
file.

**TODO**: design this!

## TODO

- **conditional run node**: It has a special subnode called the `conditional node`.
    - First, the conditional node is ran, and its return value is taken.
    - Then other regular sub-nodes are executed or not, based on the value returned.
- **custom node**: for advanced users. Custom nodes must be implemented by your own node class, using Python. Custom
  nodes can create, modify and delete sub-nodes on-the-fly.

