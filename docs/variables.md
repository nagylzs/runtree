# Variables and the environment

## Variables

`vars` sets variables for a node and the nodes below it. `{name}` in a string is replaced by the value of the
variable:

```yaml
tree:
  vars:
    host: server01
    target: test
  seq:
    - args: [ssh, "root@{host}", make, "{target}"]   # ssh root@server01 make test
```

The values are strings, or lists of strings (for [`for_vars`](tree-files.md#for_vars)). Variables can be used in these
properties: `title`, `description`, `args`, `args_prefix`, `cwd`, `envs`, `runner`, `xlocks`, `rlocks`, `provides`,
`requires`, `ifeq` / `ifneq`, the values of `for_vars`, and `vars` and `def_vars` themselves.

The substitution rules:

- A reference is `{name}`, where the name is like an identifier: `[A-Za-z_][A-Za-z0-9_-]*`. Other text in curly
  braces is left as it is, e.g. `{}`, `{a,b}` (a shell brace expansion) or `{{.Name}}`.
- A reference after a `$` is left for the shell: `${name}` and `$name` are not substituted.
- The substitution is done in a single pass: a substituted value is not substituted again.
- Only string variables can be used in strings. A list variable can only be used in `for_vars`.
- Variables are calculated once, when the tree is loaded. The values do not change while the tree runs.

### Variables that use each other

The values in a `vars` block can use the inherited variables, and the other variables of the same block, in any
order and through any number of steps (but not in a cycle, that is an error). A variable of the block overrides the
inherited one with the same name, also in the other values of the block. In its own value, a variable means its
inherited value, so it can be extended:

```yaml
outer:
  vars:
    target: build
    path: /usr/bin
  seq:
    - vars:
        command: "make {target}"     # "make test": target of this block overrides "build"
        target: test
        path: "{path}:/opt/tools/bin" # "/usr/bin:/opt/tools/bin": {path} is the inherited value here
      envs:
        PATH: "{path}"
      args: [bash, -c, "{command}"]
```

### Default variables

`def_vars` gives defaults: they are only used when the variable is not set already (inherited, or by the `for_vars` of
the parent). They are meant for [included](tree-files.md#include) objects, for the variables that are usually set
where the object is included:

```yaml
deploy:
  def_vars:
    host: localhost
    port: "8080"
    url: "http://{host}:{port}"
  args: [curl, "{url}"]
```

The values in `def_vars` can use the inherited variables and the other defaults of the same block, with their final
values: above, `url` uses the inherited `host` when there is one, otherwise the default. They cannot use the `vars` of
the same node (the defaults stand in for values given from outside, and those cannot depend on the node itself); put
the node into a seq node with those `vars` when it is needed. A variable cannot be in both `vars` and `def_vars` of
the same node.

### Inheritance

`inherit_vars: false` makes a node start without the variables of its parent: it only has its own `vars` (and the
`for_vars` combination of its parent, when there is one). `def_vars` cannot be used then.

The variables of a node are calculated like this:

1. The parent is calculated before its sub-nodes.
2. With `inherit_vars` (the default), the variables of the parent are the base; otherwise the base is empty.
3. The `for_vars` variables of the parent (the combination that the node was generated for) are added to the base.
4. `def_vars` are added for the variables that are not in the base.
5. `vars` are added, they override the variables of the base.

Inside a node, `vars` and `def_vars` are calculated before the other properties, wherever they are in the object.

### Where the values are calculated

The inherited properties (`args_prefix`, `cwd`, `envs`, `runner`, `rlocks`...) are calculated in the node that sets
them, with the variables of that node, and the sub-nodes inherit the result. A variable that only exists below that
node is not visible there. In particular, the `for_vars` of a node are variables of its sub-nodes only:

```yaml
# Wrong: {server} is not a variable of this node, the sub-nodes get "root@{server}"
- for_vars: {server: [web01, web02]}
  args_prefix: [ssh, "root@{server}"]
  par: [[uptime]]

# Right: the prefix is set in the repeated sub-node
- for_vars: {server: [web01, web02]}
  par:
    - args_prefix: [ssh, "root@{server}"]
      seq: [[uptime], [df, -h]]
```

### Undefined variables

A reference to a variable that is not set is left as it is (`{name}`), and `runtree --check` reports it as a warning
(see [Checking a tree file](tree-files.md#checking-a-tree-file)). It is only a warning, because a command may need a
`{name}` text; tag those strings `!raw`.

### Raw strings

A string that is tagged `!raw` is used as it is, without substitution and without undefined variable warnings:

```yaml
args: [bash, -c, !raw "jq '{name: .user}' data.json"]
```

The tag applies to the whole string. It can be used for any string value (in `args`, `title`, `envs`, `vars`, in a
list variable...), but not for object keys, lists or objects. Other tags are errors.

## Environment

The environment of a process is calculated like this:

1. With `system_envs` (the default), the base is the environment of the [runner](runners.md): of `runtree` for the
   local runner, of the `rtrunner` process for ssh and TCP runners (for an ssh runner, the environment of the ssh
   login on the host). With `system_envs: false`, the base is empty.
2. With `inherit_envs` (the default), the `envs` of the parent node are added, otherwise they are not.
3. The node's own `envs` are added, they override the others.

```yaml
- title: "query the database"
  vars:
    host: db01
    user: report
  envs:
    PGHOST: "{host}"
    PGUSER: "{user}"
  args: [psql, -f, report.sql]
```

The values of `envs` can use variables, and `{NAME}` of the inherited `envs` (a variable with the same name wins).
The environment of the runner cannot be referenced this way (`{HOME}` stays as it is): use the shell for it, e.g.
`args: [sh, -c, 'cd "$HOME" && make']`.

With `system_envs: false`, define what the program needs (e.g. `PATH`, `HOME`). The program itself is still looked up
in the `PATH` of the runner, the `PATH` in `envs` is what the process sees.

When a command runs `ssh` itself (on the local runner), its `envs` are set for the local `ssh` process, and ssh does
not pass them to the host. Use an [ssh runner](runners.md#ssh-runners) to set the environment on the host, or set the
variables in the remote command (`NEEDRESTART_MODE=l apt-get -y dist-upgrade`).

## Working directory

`cwd` is the directory where the processes start. It is inherited. When it is not set (or empty), the processes start
in the current directory of the runner: where `runtree` was started for the local runner, the home directory of the
user for an ssh runner (so there a relative path is relative to the home directory).

`~` is not expanded: no shell starts the processes, and `cwd` applies on the host of the runner, for its user. A
`cwd` that starts with `~` is an error when the tree is loaded (`--check` reports it too), instead of a failed start
later: use an absolute path, or for an ssh runner a relative one (`deploy` instead of `~/deploy`).

## Command prefix

`args_prefix` is put before the `args` of the run nodes in a subtree. It is inherited, so a whole subtree can run
through ssh with one line:

```yaml
- title: "web01"
  args_prefix: [ssh, -t, root@web01]
  seq:
    - [apt-get, update]
    - [apt-get, -y, dist-upgrade]
    - title: "wait until it answers ping (here)"
      args_prefix: []            # no prefix: this one runs locally
      args: [sh, -c, "until ping -c 1 web01; do sleep 1; done"]
```

A sub-node with its own `args_prefix` (also an empty one) replaces the inherited prefix.
