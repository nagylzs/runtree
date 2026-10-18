# Writing tree files

A tree file is a YAML file. Its top level object `tree` is the root node of the tree that runtree runs; the other top
level objects are building blocks that the tree can [include](#include). Name the files `*.rt.yaml` (e.g.
`upgrade_servers.rt.yaml`) so that [editors](editors.md) recognize them; runtree itself accepts any name.

```yaml
tree:
  title: "Deploy"
  seq:
    - include: build
    - [sh, -c, "echo deploying; sleep 2"]

build:
  title: "Build"
  args: [make, build]
```

The [node properties reference](reference/properties.md) lists every property with its value type and default.

## Nodes

There are three types of nodes:

| Type | What it does |
|------|--------------|
| `run` | runs a command (a process) in a terminal |
| `seq` | runs its sub-nodes one after the other |
| `par` | runs its sub-nodes in parallel |

Only run nodes start processes. A `seq` or `par` node gets its status from its sub-nodes (see
[How a tree runs](execution.md)).

The type is usually not written out:

- a node with `seq:` or `par:` is a seq or par node, and that property lists its sub-nodes;
- a node with `include` (and without `type`) is a seq node;
- every other node is a run node (`type: run` is the default).

With `type`, the sub-nodes are given in `nodes`:

```yaml
tree:
  type: par
  nodes:
    - args: [make, docs]
    - args: [make, tests]
```

Every node can have:

- `id`: an identifier, it must match `[a-zA-Z][a-zA-Z0-9_-]*` and be unique in the tree. When it is not given, a
  generated id is used (`n1`, `n2`, ...). The ids appear in the GUI and in the [logs](logging.md).
- `title`: a single line that the GUI shows. Without a title, a run node shows its command line.
- `description`: a longer text, shown in the Description tab of the GUI. YAML block scalars (`|`) are handy for it.

Variables can be used in the title and the description (see [Variables](variables.md#variables)).

## Run nodes

A run node runs the command in `args`: the program and its arguments, as a list of strings.

```yaml
- title: "Upgrade the packages"
  args: [apt-get, -y, dist-upgrade]
```

The command is not given to a shell: the program is looked up in the `PATH` of the [runner](runners.md), and the
arguments are passed as they are. For shell features (pipes, `&&`, redirections, variables), run a shell:

```yaml
- args: [sh, -c, "apt-get update && apt-get -y dist-upgrade"]
```

`args` is not inherited, but [`args_prefix`](variables.md#command-prefix) puts common arguments before the `args` of
every run node in a subtree (e.g. `[ssh, root@server01]`).

A run node succeeds when its process exits normally with exit code 0 (or with another code listed in
[`success_exit_codes`](execution.md#exit-codes)). Otherwise its [`on_error`](execution.md#errors) decides what
happens.

## Short forms

A sub-node that is a list of strings is a run node with those `args`:

```yaml
tree:
  seq:
    - [make, build]
    - [make, test]
```

The shortest form puts the list of lists after `seq` or `par` directly:

```yaml
tree:
  on_error: paused
  seq: [[make, build], [make, deploy]]
```

## Include

`include` loads top level objects as sub-nodes. Its value is a name, or a list of names:

- `name`: a top level object of the same file;
- `file|name`: a top level object of another file. The path is relative to the file that has the `include`.

```yaml
tree:
  par:
    - include: build
    - include: "lib/checks.rt.yaml|smoke_test"
```

The included objects are full nodes: they can have sub-nodes, and include other objects (of their own file by name).
An object is calculated where it is included: it inherits the variables and the other inherited properties of the
including node, so the same object can be included several times with different variables.
[`def_vars`](variables.md#default-variables) give the defaults of the variables that are not set where an object is
included.

A node with `include` cannot have `nodes` too, and a run node cannot include. Includes cannot be circular: a tree
deeper than `--max-depth` (30 by default) is an error ("maximum depth exceeded (are the includes circular?)").

### Including one object

When a node only includes one object (and it has no `for_vars`, `xlocks` and `rlocks`), the object is loaded into
the node, instead of becoming its sub-node: the node becomes the object (e.g. a run node). The properties of the
including node that the object inherits (`vars`, `envs`, `cwd`, `runner`, `on_error`...) apply to it, and the node
keeps its own `title`, `description`, `provides` and `requires`:

```yaml
tree:
  seq:
    - title: "Upgrade web01"     # the title of this node
      vars: {host: web01}
      include: upgrade_host      # the node is the upgrade_host seq, for web01
    - title: "Upgrade web02"
      vars: {host: web02}
      include: upgrade_host

upgrade_host:
  seq:
    - [ssh, "root@{host}", apt-get, update]
    - [ssh, "root@{host}", apt-get, -y, dist-upgrade]
```

## for_vars

`for_vars` repeats the sub-nodes of a node for every combination of the given values:

```yaml
tree:
  vars:
    targets: [dev, uat]
  for_vars:
    server: [server01, server02, server03]
    target: targets             # the name of a list variable
  par:
    - args: [ssh, "root@{server}", make, build, "{target}"]
```

The values are lists of strings, or the name of a variable that is a list of strings. The strings can use the
variables of the node (e.g. `server: ["{env}-01", "{env}-02"]`). The combinations are made in the order the variables
are written, the first one changes the slowest (like nested loops), so the example gives these six run nodes:

```yaml
- args: [ssh, root@server01, make, build, dev]
- args: [ssh, root@server01, make, build, uat]
- args: [ssh, root@server02, make, build, dev]
- args: [ssh, root@server02, make, build, uat]
- args: [ssh, root@server03, make, build, dev]
- args: [ssh, root@server03, make, build, uat]
```

`for_vars` works on the sub-nodes, so it can only be used on a node that has sub-nodes (`seq`, `par`, `nodes` or
`include`). Its variables are set for the sub-nodes like inherited variables: the `vars` of a sub-node override them,
and its `def_vars` are not used for them. They are not visible in the node itself: an `args_prefix` that uses
`{server}` must be set in the sub-nodes, not next to the `for_vars` (see [where the values are
calculated](variables.md#where-the-values-are-calculated)).

When a list is empty, there are no combinations, the node has no sub-nodes, and `--check` warns about it.

## Conditions

`ifeq` keeps a node in the tree only when the variables have the given values; `ifneq` only when they differ from
them. The values can use variables, and an undefined variable is empty.

```yaml
deploy:
  seq:
    - [sh, -c, "echo copy the release to {server}"]
    - title: "migrate the database"
      ifeq: {env: uat}
      args: [sh, -c, "echo migrating {env}"]
    - title: "restart"
      ifneq: {env: dev}
      args: [sh, -c, "echo restarting {server}"]
```

With several variables, all of them must match (`ifeq`) or differ (`ifneq`). The condition is checked when the tree
is loaded: a node that is left out is not in the tree at all.

## YAML details

- Tree files are read as YAML 1.2: `yes`, `no`, `on` and `off` are strings (e.g. in `args`), but boolean properties
  accept them too.
- Anchors, aliases and merge keys (`<<: *anchor`) can be used. A key must not be repeated in an object.
- A file can be 1 MiB at most.
- In a double-quoted string that spans several lines, YAML joins the lines with spaces (an empty line gives a line
  break). A shell command written over several lines therefore needs its `;` or `&&` separators, see
  [pitfalls](practice.md#pitfalls).

## Checking a tree file

`runtree --check tree.rt.yaml` loads the tree file and the files that it includes, and exits without starting the GUI
or any process. It prints all errors with their positions, and the exit code is 1 when there is an error:

```
servers.rt.yaml:7:7: unknown property "on_eror" (did you mean "on_error"?)
  in node: upgrade all › upgrade servers › deploy
  included from main.rt.yaml:5:17 (include "servers.rt.yaml|upgrade")
servers.rt.yaml:12:16: invalid args: item 2 is a number, expected a string (put it in quotes)
  in node: upgrade all › upgrade servers › reboot
  included from main.rt.yaml:5:17 (include "servers.rt.yaml|upgrade")
2 errors
```

- The first line of each error has the usual `file:line:column: message` format, so editors (e.g. a VS Code task)
  can jump to it.
- The errors are sorted by their position, the files in the order they are loaded.
- An error that is found several times (e.g. in an object that is included more than once, or under `for_vars`) is
  reported once, with "(found N times)" and the include chain of its first occurrence.

Warnings are printed the same way, with `warning:` before the message. They do not prevent running the tree; when the
GUI is started, a banner above the tree tells that there are warnings (its **Show warnings** button lists them).

```
lib.rt.yaml:2:15: warning: undefined variable {user}, it is not set where it is included
  in node: deploy › job
  included from main.rt.yaml:6:16 (include "lib.rt.yaml|job")
main.rt.yaml: OK, 5 nodes, 1 warning
```

The warnings are:

- `undefined variable {name}`: a value refers to a variable that is not set for the node. The reference is left as it
  is, e.g. the command gets `{user}@host` as an argument. In an included object it usually means that the variable
  should be set where the object is included (with `vars` or `for_vars` of a parent node, or with `def_vars` of the
  object). See [undefined variables](variables.md#undefined-variables).
- `for_vars: name is an empty list`: there are no combinations, so the node has no sub-nodes.

The GUI does the same checks when it starts: it does not start a tree that has errors.
