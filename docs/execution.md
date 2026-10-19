# How a tree runs

## The scheduler

When runtree starts, the scheduler starts the run nodes of the tree as soon as they can be started. It works in
cycles: after every change (a process started or exited, an operation in the GUI), and at least every second. In a
cycle it updates the statuses, and it looks for a node to start, from the root, depth first:

- in a `seq` node, it looks into the first sub-node that is not finished, and it does not go further;
- in a `par` node, it looks into every sub-node, in order;
- it does not look into a node that is finished, [frozen](#frozen-nodes), paused, [blocked](#blocking) (by
  `requires` or locks), or that has reached its [`max_proc`](#limiting-processes);
- a waiting run node that it finds is started.

One node is started in a cycle, and starting it starts the next cycle, so the nodes of a `par` node start right after
each other. Starting a process (connecting to the runner, e.g. an ssh login) happens in the background: meanwhile the
node is Running, the GUI shows "connecting".

The scheduler only starts nodes. It never stops, freezes or skips anything by itself, except through
[`on_error`](#errors). When the tree is finished, runtree keeps running until you close the window.

## Statuses

| Status | Meaning | Active | Finished | Error | Waits for you |
|--------|---------|:------:|:--------:|:-----:|:-------------:|
| Waiting | not started yet, the scheduler starts it when it can | | | | |
| Frozen | not started, the scheduler does not start it until it is thawed | | | | yes |
| Running | its process runs (a seq or par node: something below it is running or not finished) | yes | | | |
| Paused | its process (or a sub-node) failed, and its `on_error` is `paused`: you choose what happens | | | | yes |
| Success | finished successfully | | yes | | |
| Failed | finished with an error | | yes | yes | |
| Skipped | it will not be started (by the Skip operation, or `on_error: skip` of a sibling) | | yes | | |

- **Active** nodes hold their [locks](coordination.md#locks) (paused nodes hold them too), and count in
  [`max_proc`](#limiting-processes).
- **Finished** nodes are final for the scheduler, only an [operation](gui.md#operations) can change them (e.g.
  Re-run, Reset to waiting). A finished node without error (Success, Skipped) [provides](coordination.md#dependencies)
  its resources.
- A node that failed with `on_error: success` is Success: the scheduler goes on as if it had succeeded, but its error
  is kept, and the GUI shows it.

### Run nodes

A run node is Waiting until the scheduler starts it, then Running while its process runs. When the process exits:

- it is **Success** if the process exited normally (not by a signal) with an exit code in
  [`success_exit_codes`](#exit-codes) (by default 0), and it did not run longer than its [`timeout`](#timeouts);
- otherwise it gets the status of its [`on_error`](#errors): Failed (the default), Paused or Success.

A process that could not be started (e.g. the runner is not reachable, or the program is not found) is a start
error, the node gets its `on_error` status the same way.

### seq and par nodes

A seq or par node gets its status from its sub-nodes:

- It is Running from the moment one of its sub-nodes becomes active, until all of them are finished. So it can be
  Running while none of its sub-nodes runs (e.g. some are finished, the others are frozen or blocked). This is
  intentional: a seq or par node holds its locks for the whole group of its sub-nodes.
- When all of its sub-nodes are finished, it is Success if none of them has an error, otherwise it gets the status of
  its `on_error` (Failed by default).
- Before it starts, it is Frozen when its unfinished sub-nodes are all frozen (e.g. by the `freeze` sibling operation
  of [`on_error`](#errors)): nothing below it starts until they are thawed (**Thaw all**).
- A **seq** node stops at its first failed sub-node: it gets its `on_error` status right away, and its next
  sub-nodes are not started. A sub-node that is allowed to fail should have `on_error: success`.
- A **par** node keeps running its other sub-nodes when one of them fails (they are usually independent), and it gets
  its final status when all of them are finished. Meanwhile the GUI shows the number of failed run nodes below it
  (e.g. `Running (2), 1 failed`). To stop starting new sub-nodes after a failure, use the sibling operation of
  [`on_error`](#errors).
- Its status can also be set by hand: a paused seq or par node can be set to Success or Failed (this status lasts until
  an operation is performed on the node or below it), and one that is not started yet can be skipped (it is Skipped
  until it is reset, and its sub-nodes are not started).

## Errors

`on_error` says what a node does when its process, or one of its sub-nodes, fails. It has two parts:

- the **status** of the node: `failed` (the default), `paused` (it waits for you to choose: Re-run, Success or Fail in
  the GUI; [`runtree-headless`](headless.md#nobody-decides) has nobody to ask, the node fails there), or `success`
  (the failure is allowed);
- an **operation on the siblings** that are not started yet: `none` (the default), `freeze` (they are frozen, thaw
  them to go on) or `skip` (they are skipped).

```yaml
on_error: paused                 # the status only
on_error: skip                   # the operation only (the status is failed)
on_error: [paused, freeze]       # both
on_error: "paused, freeze"       # the same, as a string
```

The part that is not given is the default (`failed` or `none`), not the inherited value.

`on_error` is inherited. `children_on_error` sets the value that the sub-nodes inherit, when the node itself should
behave differently from its sub-nodes. For example, a seq node that pauses when one of its steps fails, while the
steps (and everything below them) simply fail:

```yaml
- title: "Upgrade the OS"
  on_error: paused
  children_on_error: failed
  seq: [...]
```

The sibling operation is performed when the node gets its error status, on the siblings that are not started yet
(waiting or frozen); the siblings that are already running finish their work. A rolling upgrade, one server at a
time, that stops at the first failed server:

```yaml
- title: "Upgrade the servers"
  max_proc: 1
  children_on_error: [failed, freeze]
  par:
    - [ssh, root@server1, apt-get -y dist-upgrade]
    - [ssh, root@server2, apt-get -y dist-upgrade]
    - [ssh, root@server3, apt-get -y dist-upgrade]
```

When server2 fails, server3 is frozen, and the par node stays Running until you thaw server3 (to go on) or skip it
(to finish).

`on_error: [success, skip]` makes a step a condition for its next siblings: when it fails (e.g. `test -f
/var/run/reboot-required` exits with 1), it counts as successful, and the following steps of its seq are skipped.
See [patterns](practice.md#patterns).

## Exit codes

`success_exit_codes` lists the exit codes that mean success. It is inherited, the default is `[0]`. The listed codes
replace the default, so list 0 too when it should also mean success:

```yaml
- title: "reboot"
  success_exit_codes: [0, 255]   # ssh exits with 255 when the rebooting host closes the connection
  args: [ssh, root@server1, reboot]
```

A process that was killed by a signal is always a failure.

## Timeouts

`timeout` is the longest time that a run node may run, e.g. `90s`, `30m` or `1h30m` (Go durations: `h`, `m`, `s`,
`ms`; a number needs a unit). When the node runs longer (counted from its start, like its elapsed time in the GUI),
runtree stops its process like you would:

1. it types Ctrl+C into its terminal: the terminal sends SIGINT to the process and to the commands that it runs, and
   `ssh -t` passes it to the remote command, so it stops there too;
2. SIGTERM after 10 seconds, if it still runs;
3. SIGKILL after 10 more seconds.

The signals go to the process group of the process, like Ctrl+C. Then the node fails, also when the process exits
with a success code after Ctrl+C (it did not finish its work): it gets the status of its [`on_error`](#errors), e.g.
Paused, so in the GUI you can look at its terminal and decide (Re-run, Success, Fail), and `runtree-headless` goes on
with the rest of the tree instead of waiting forever. The status change tells why (`Running → Paused (timed out after
30m)`), the node shows "timed out" until it is started again, and its Re-run starts the clock again.

`timeout` is inherited, and `0` (the default) means no limit. On a seq or par node it is the limit of each run node
below it, not of the group, so a timeout on the root node is a safety net for every step of the tree, and the slow
steps can have more:

```yaml
tree:
  timeout: 30m                 # every step of the tree
  seq:
    - title: "db updates"
      timeout: 3h              # this one can take long
      args: [ssh, -t, root@db1, /usr/local/sbin/update-all.sh]
    - title: "shell for the admin"
      timeout: 0               # no limit
      args: [bash]
```

A step that needs another way to stop (another signal, or less time between them) can do it itself, e.g. with
`timeout --signal=HUP --kill-after=1m 20m command` of coreutils in its `args`.

## Limiting processes

`max_proc` limits the number of processes that run at the same time in a subtree:

```yaml
- title: "Download"
  max_proc: 4
  par: [...]
```

When the number of running processes below a node reaches its `max_proc`, the scheduler does not start new ones in
that subtree until a process finishes. A negative value (the default is -1) means no limit, and 0 means that nothing
is started in the subtree. `max_proc` is inherited: a sub-node without its own value applies the same limit to its own
subtree.

`max_proc` only limits the scheduler: you can still start nodes by hand.

## Frozen nodes

`status: frozen` makes a node frozen when the tree is loaded: the scheduler does not start it until you **Thaw** it
(or **Skip** it). It is a breakpoint, or an approval gate, in the tree:

```yaml
- title: "approve the production deploy"
  status: frozen
  args: [echo, approved]
```

`status` is inherited, so the sub-nodes of a frozen seq or par node are frozen too: use **Thaw all** for them.
**Thaw** only thaws the seq or par node itself, so it is still Frozen while its sub-nodes are (it only starts the
sub-nodes that have `status: waiting`). **Skip** skips the whole group. A frozen run node can also be started by hand
with **Run**. The possible initial statuses are `waiting` (the default) and `frozen`. `runtree-headless` never thaws
a node: its run [stops](headless.md#when-the-run-ends) when only frozen nodes are left.

## Blocking

A waiting node is **blocked** when it cannot start yet:

- a resource in its `requires` is not provided (see [dependencies](coordination.md#dependencies));
- one of its locks is held by another node (see [locks](coordination.md#locks)).

The scheduler does not start a blocked node, and it does not look into the subtree of a blocked seq or par node. The
GUI shows why a node waits in its status ("blocked", "max proc reached", "won't schedule" for frozen and paused
nodes), and the Dependencies tab shows what it waits for. A tree can deadlock (e.g. two nodes that require what the
other one provides): runtree does not detect it, it simply waits (`runtree-headless` stops then).
