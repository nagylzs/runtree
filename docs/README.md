# runtree documentation

runtree runs the steps of an operations task (an upgrade of many servers, a deployment, a build pipeline) as a tree
of commands. The tree says what runs in order and what runs in parallel, what waits for what, and what to do when a
step fails. Each command runs in its own terminal, which you can watch and type into, and a GUI shows the state of the
whole tree and lets you intervene: pause, fix, re-run, skip, or continue.

## User guide

Read these in order the first time; each page builds on the previous ones.

1. [Getting started](getting-started.md): install runtree and run the first trees
2. [Writing tree files](tree-files.md): nodes, `run` / `seq` / `par`, short forms, `include`, `for_vars`, conditions,
   checking a tree
3. [Variables and the environment](variables.md): `vars`, `def_vars`, substitution, raw strings, `envs`, `cwd`,
   `args_prefix`
4. [How a tree runs](execution.md): statuses, the scheduler, errors and `on_error`, exit codes, `max_proc`, frozen
   nodes
5. [Dependencies and locks](coordination.md): `provides` / `requires`, `xlocks` / `rlocks`
6. [The GUI](gui.md): the tree, the terminals, the operations, closing runtree
7. [Runners](runners.md): where the processes run: locally, over ssh, or on an `rtrunner` daemon
8. [Logs and history](logging.md): events, the Log tab, saving the runs in SQLite
9. [Running trees in practice](practice.md): lock files, signals and exit, several users, patterns and pitfalls
10. [Running without the GUI](headless.md): `runtree-headless` in CI and cron, exit codes, output

The [examples](../examples/README.md) go from the simplest tree to a realistic rolling upgrade.

## Reference

- [Node properties](reference/properties.md): every property of the tree files (generated from the source)
- [Command line](reference/cli.md): the options of `runtree`, `runtree-headless`, `rtrunner` and `runtree-lsp`, exit
  codes, files
- [Events](logging.md#events): the events of the log
- [Editor support](editors.md): the JSON Schema, the language server and the VS Code extension

## For developers

- [Development](development.md): building, tests, CI, the structure of the code
