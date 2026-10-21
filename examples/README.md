# Examples

Example trees, from the simplest one to a realistic rolling upgrade. Each file starts with a comment that explains
what it shows and how to run it. They only run harmless commands (`echo`, `sleep`, `sh`) on the local machine,
except `10-runners`, which can use ssh to localhost, and `12-windows`, which runs Windows commands (cmd, PowerShell,
robocopy, ping).

| File | Shows |
|------|-------|
| [01-hello](01-hello.rt.yaml) | the smallest tree: one run node |
| [02-steps](02-steps.rt.yaml) | `seq`: steps one after the other, the short form of run nodes, a failed step |
| [03-parallel](03-parallel.rt.yaml) | `par`: parallel steps, `max_proc`, nesting seq and par |
| [04-variables](04-variables.rt.yaml) | `vars`, `envs`, `cwd`, `args_prefix`, `!raw` strings |
| [05-errors](05-errors.rt.yaml) | `on_error`, `children_on_error`, `success_exit_codes`, stopping a rollout |
| [06-reuse](06-reuse.rt.yaml) | `include` (also from [another file](lib/deploy.rt.yaml)), `for_vars`, `def_vars`, `ifeq` / `ifneq` |
| [07-dependencies](07-dependencies.rt.yaml) | `provides` and `requires` across branches |
| [08-locks](08-locks.rt.yaml) | `xlocks` and `rlocks` |
| [09-breakpoints](09-breakpoints.rt.yaml) | frozen nodes as breakpoints, interactive terminals, manual operations |
| [10-runners](10-runners.rt.yaml) | `runner`: local, `ssh://`, `--runner` |
| [11-rolling-upgrade](11-rolling-upgrade.rt.yaml) | all of them together: a rolling upgrade of web servers |
| [12-windows](12-windows.rt.yaml) | Windows: cmd, PowerShell, the exit codes of robocopy, `timeout` (Ctrl+C) |

```bash
runtree --check examples/06-reuse.rt.yaml   # check a tree without running it
runtree examples/01-hello.rt.yaml           # run it in the GUI
```

The test suite loads all of them (`TestExampleTrees`), so they stay valid.
