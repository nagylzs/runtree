# Running without the GUI

`runtree-headless` runs a tree without the GUI, e.g. as a CI step, from cron, or on a server over ssh. It is a
separate program, without GTK and VTE, so it runs where the GUI cannot: on a server without a display, on a CI image
without the GTK libraries, and on Windows.

```bash
runtree-headless deploy.rt.yaml
runtree-headless --lock /srv/runtree/deploy.lock --log-db /srv/runtree/runs.db deploy.rt.yaml
```

It loads and runs the tree like `runtree` (the same [options](reference/cli.md#runtree-headless) for the runners,
the lock file and the log database), and it exits when the run ends. Its [exit code](#exit-codes) tells how it ended.

## Nobody decides

Nobody watches a headless run, so nobody can make the decisions of the GUI:

- **A node that would pause fails.** `on_error: paused` means "ask what to do"; without anybody to ask, the node is
  Failed, as if you pressed Fail: its seq stops, and the `on_error` of its parent decides what happens next. The status
  change tells why: `Running → Failed (on_error is paused, but nobody can decide: the tree runs unattended)`. The
  sibling operation of `on_error` still happens: `[paused, freeze]` works like `[failed, freeze]`.
- **Frozen nodes stay frozen.** Nobody thaws `status: frozen` approval gates, or the siblings that `on_error` froze:
  they are not run, and not skipped either.
- **Nobody types.** The processes have terminals, but nothing is typed into them (see [limitations](#limitations)).

## When the run ends

The run ends when the root node is finished (Success or Failed), or when it **stops**: nothing runs, and nothing can
be started anymore, e.g. the rest of the tree is frozen, or it waits for a resource that nothing provides, or for a
lock that a group holds while its next step is frozen. Then `runtree-headless` lists what the tree waits for:

```
The run stopped after 2m14s: nothing runs, and nothing can be started.
Waiting for:
  web02 upgrade: frozen (by on_error)
  web03 upgrade: frozen (by on_error)
Failed:
  web01 apt dist-upgrade: exit code 100
```

The decision is deterministic. In a headless run only the processes can change the tree (there are no operations),
so when no process runs or starts, the scheduler updates the statuses once more, and when nothing has changed and
there is still nothing to start, nothing ever will: the run stopped. A tree that the GUI would wait in forever (e.g. a
deadlock of `requires`) stops the same way.

## Output

The terminal output of the nodes goes to stdout, line by line, with the label of the node in front of each line:
its title, or its command, or its id, shortened to 30 characters. The labels are padded to the same width:

```
web01 apt update       | Hit:1 http://deb.debian.org/debian trixie InRelease
web02 apt update       | Reading package lists...
```

- A line ends with a new line, or with a carriage return (`\r`): a progress bar that redraws its line writes a line
  for each state.
- The colors pass through (they are reset at the end of the line).
- A line that does not end, e.g. a prompt that waits for input, is written after 2 seconds.

The events of the tree go to stderr, one line each, with the time and the label of the node (the level is there when
it is not info):

```
10:30:01 web01 apt update: starting: apt-get update
10:30:05 web01 apt update: exited with code 0 after 4.1s
10:30:05 web01 apt update: Running → Success
10:30:09 WARN web02 apt update: exited with code 100 after 8s
```

`--log-format json` writes them as JSON objects instead, with all their attributes (see [events](logging.md#events)),
and `-d` adds the debug messages. At the end, a summary goes to stderr: how the run ended, what it waits for when it
stopped, and the failed run nodes with their exit codes.

`--log-db` saves the events and the output in the [log database](logging.md#saving-the-log), as `runtree` does; the
GUI's `runtree --show-output NODE` prints the output of a node from there.

## Exit codes

| Code | When |
|------|------|
| 0 | the root node is Success (the nodes with `on_error: success` may have failed) |
| 1 | the tree has errors (or a command line error) |
| 2 | the root node is Failed |
| 3 | the run stopped: nothing runs, and nothing can be started (also when some nodes failed) |
| 75 | the lock file is locked by another runtree (see [lock files](practice.md#running-a-tree-once-at-a-time)) |
| 130 | SIGINT (Ctrl+C) |
| 143 | SIGTERM |

## Signals

SIGINT (Ctrl+C) and SIGTERM stop the run: `runtree-headless` stops starting nodes, and terminates the processes that
still run like `runtree` does when it exits (SIGINT, then SIGTERM after 10 seconds and SIGKILL after 10 more), then it
prints the summary. A second signal exits at once. On Windows the processes are killed at once (there are no
signals).

## In CI and cron

A CI step (e.g. GitHub Actions) fails when the tree fails or stops:

```yaml
- name: Deploy
  run: runtree-headless --log-db runs.db deploy.rt.yaml
```

From cron, with a lock file, so that a slow run is not started twice (exit code 75 is not an error then):

```
*/30 * * * * runtree-headless --lock /srv/runtree/sync.lock /srv/runtree/sync.rt.yaml >>/var/log/sync.log 2>&1
```

## Limitations

- **Input.** Nothing is typed into the terminals, so a command that asks something (a password of `sudo`, the host
  key of `ssh`, a confirmation of `apt`) waits forever, and the run never ends. Use the non-interactive options of the
  commands: `apt-get -y`, `sudo -n`, `ssh -o BatchMode=yes`. The prompt is in the output (after 2 seconds), so you can
  see what it waits for. There is no timeout for the nodes yet.
- **A process that never exits** holds up the run the same way.
- **Windows.** `runtree-headless` and the local runner work on Windows (the processes run in ConPTY pseudo consoles,
  whose output has more terminal control sequences than a Linux terminal's), but the ssh runners do not, and it has
  been tested under wine only.
