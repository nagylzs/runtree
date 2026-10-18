# Running trees in practice

## Running a tree once at a time

`runtree --lock FILE tree.rt.yaml` locks `FILE` while the tree runs. Another runtree with the same lock file does not
start: it prints who holds the lock, and exits with 75 (`EX_TEMPFAIL`, so a script can try again later):

```
the tree cannot be run now: /srv/runtree/upgrade.lock is locked, the lock file says: pid 12345,
admin@deploy01 since 2026-10-18 10:30:00 +0200, tree /srv/runtree/upgrade.rt.yaml
```

- It is a lock of the kernel (`flock`), not only a pid file: it is released when runtree exits in any way, also when
  it is killed or crashes, so a lock is never left behind. It is held until the processes have exited (see
  [signals and exit](#signals-and-exit)).
- The first line of the file is the pid of the runtree that holds the lock (`kill $(head -1 FILE)` works), the second
  one is who started it, from where, when, and the tree file.
- The file is never deleted, and several trees can use the same one (e.g. all the trees that change the same
  servers).
- `--check` and the `--show-*` options do not use the lock.

## Several users

When several people run the same trees, run them on one machine (e.g. a deploy host that everyone logs in to with
ssh), with a shared lock file and log database:

```bash
runtree --lock /srv/runtree/upgrade.lock --log-db /srv/runtree/runs.db /srv/runtree/upgrade.rt.yaml
```

- The users must be able to open the lock file: reading is enough to lock it, writing is needed to record who holds
  it (otherwise the file says who held it before). Create it once for a group:
  `touch /srv/runtree/upgrade.lock; chgrp ops /srv/runtree/upgrade.lock; chmod 664 /srv/runtree/upgrade.lock`.
- The log database can be shared the same way (it must be writable by the group, and on a local filesystem); every
  session is a run in it, with the host and the user.
- The lock only works between the runtree processes of the same machine. Locks on network filesystems (NFS) may not
  work between machines, and runtrees on different machines (e.g. on the laptops of the users) cannot coordinate with
  a lock file.

## Signals and exit

| Event | What runtree does |
|-------|-------------------|
| the window is closed | asks for confirmation when there are run nodes that are not Success or Skipped, then exits |
| SIGINT (Ctrl+C in its terminal), SIGHUP (its terminal is closed) | the same as closing the window |
| SIGTERM | exits without confirmation (with exit code 143) |
| SIGKILL, or a crash | exits at once, see below |

When runtree exits, it stops starting nodes, and it terminates the processes that still run: SIGINT, then SIGTERM after
10 seconds and SIGKILL after 10 more, and it waits until they exit (their exits are logged, and saved in the log
database). The window closes right away; runtree prints what it waits for on its terminal. When it is killed, the
processes of the local runner get SIGHUP when their terminals close, and the remote ones are terminated by their
`rtrunner` when the connection is gone.

The exit codes of runtree:

| Code | When |
|------|------|
| 0 | the window was closed |
| 1 | the tree has errors (or a command line error) |
| 75 | the lock file is locked by another runtree |
| 143 | SIGTERM |

The exit code does not depend on the result of the tree yet (see the TODO list for a headless mode).

## Patterns

**Stop a rollout at the first failure.** Upgrade a few servers at a time, and stop starting new ones when one fails;
the ones that are being upgraded finish their steps:

```yaml
- title: "upgrade the servers"
  max_proc: 2
  children_on_error: [paused, freeze]
  for_vars:
    host: [web01, web02, web03, web04]
  par:
    - include: upgrade_host
```

The failed server pauses (look at its terminal, fix it, re-run its failed step or set it to Success), the servers that
were not started yet are frozen: thaw them to go on, or skip them.

**Optional steps.** A step that decides whether the next steps of its seq are needed: when it fails, it counts as
successful, and its next siblings are skipped:

```yaml
- title: "reboot if needed"
  seq:
    - title: "is a reboot needed?"
      on_error: [success, skip]
      args: [test, -f, /var/run/reboot-required]   # exits with 1 when no reboot is needed
    - [shutdown, -r, now]
    - title: "wait until it is back"
      runner: local
      args: [sh, -c, "sleep 10; until nc -z -w1 {host} 22; do sleep 1; done"]
```

Note the direction: the condition succeeds when the next steps are needed. `test ! -f` would skip the reboot exactly
when it is needed.

**Approval gates.** A frozen node stops the tree at that point until someone thaws it:

```yaml
- title: "approve the production deploy"
  status: frozen
  args: [echo, approved]
```

**Local steps in a remote subtree.** Waiting for a host (ping, port checks) must run on the machine of runtree, not on
the host that is rebooting: set `runner: local` (with ssh runners), or `args_prefix: []` (when the subtree runs
`ssh` through `args_prefix`).

**Commands that are expected to fail.** `success_exit_codes: [0, 255]` for `ssh host reboot` (the connection is closed
by the rebooting host), `on_error: success` for a cleanup step that may fail.

**A test run.** `runtree --runner ssh://deploy@test-vm tree.rt.yaml` runs the nodes that do not specify a runner on a
test machine, without changing the tree.

## Pitfalls

**A list of commands only reports the last one.** In `cmd1; cmd2; cmd3` (and in a shell `for` loop), the exit code is
the one of the last command: a failed `cmd1` is not noticed. Chain them with `&&`, or stop the loop at the first error:

```yaml
args: [ssh, root@web01, "lelle-add-maintenance.sh && lelle-del-prod.sh"]
args: [ssh, root@web01, "for dom in a.example b.example; do rsync -a /certs/$dom.crt backup:/certs/ || exit $?; done"]
```

Or make each command a node of its own: then runtree shows which one failed.

**YAML joins the lines of a quoted string.** A double-quoted string that spans several lines becomes one line, the
line breaks become spaces (only an empty line gives a line break):

```yaml
# This is "for dom in a b; do rsync ... done": a shell syntax error, done is an argument of rsync
args: [sh, -c, "for dom in a b; do
                rsync ...
                done"]
```

Write the `;` separators (`rsync ... || exit $?; done`), or use a block scalar (`|`), which keeps the line breaks.

**`envs` and `ssh` in the command.** With `args: [ssh, host, ...]` on the local runner, `envs` are set for the local
`ssh` process, and ssh does not pass them to the host. Use an [ssh runner](runners.md#ssh-runners), or put the
variables into the remote command.

**`for_vars` variables in inherited properties.** An `args_prefix`, `cwd` or `runner` next to a `for_vars` cannot use
the `for_vars` variables: set them in the repeated sub-nodes (see [where the values are
calculated](variables.md#where-the-values-are-calculated)).

**The default `cwd` of an ssh runner** is the home directory of the user on the host, not the directory of runtree.
