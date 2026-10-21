# Logs and history

runtree logs what happens to the tree (the events), shows them in the Log tab of the GUI, and it can save them, with
the terminal output of the processes, in an SQLite database.

## The log on the terminal

`runtree` and `rtrunner` log to stderr. By default only the warnings and the errors are logged, `-v` adds the
information messages (the events of the tree), and `-d` the debug messages too. `--log-format json` writes a JSON
object per line instead of text (e.g. for `jq`).

## Events

The log records about the tree have an `event` attribute, and a `node` attribute (the id of the node) when they are
about a node. Their message is complete for humans; the other attributes are for programs:

| Event | Level | Logged when |
|-------|-------|-------------|
| `scheduler-started` | info | the scheduler is started (`nodes`) |
| `start` | info | a command is started on a runner (`start`, `runner`, `args`, `cwd`) (1) |
| `start-failed` | error | it could not be started, e.g. the runner is not available (`start`, `error`) |
| `exited` | info / warn | the process exited (`start`, `pid`, `exit_code`, `error`, `elapsed`, `user_time`, `system_time`) (2); a status code of Windows is in hex in the message, e.g. `0xC000013A (Ctrl+C)` |
| `status` | info / warn | the status of a node changes (`from`, `to`), warn for failed and paused |
| `operation` | info / warn | an operation is performed from the GUI (`op`, `recursive`), warn when it failed |
| `signal` | info / warn | a signal is sent from the GUI, to a node that [timed out](execution.md#timeouts), or to a remote process when runtree exits (`signal`) |
| `timeout` | warn | a node ran longer than its [`timeout`](execution.md#timeouts), Ctrl+C is typed into its terminal (`timeout`) |
| `on-error` | info | the sibling operation of `on_error` is performed (`op`) |
| `provided` | info | a node provides resources (`resources`) |
| `unprovided` | info | resources are not provided anymore, by a reset to waiting or a re-run |
| `locked` | info | a node acquires its `xlocks` and `rlocks` |
| `unlocked` | info | it releases them |
| `runner-error` | error | a runner reports an error (`code`); a warning for a signal that its system does not have (e.g. SIGTERM on Windows) |
| `runner-upload` | info | `rtrunner` is uploaded to a host (`platform`, `sha256`, `bytes`, `elapsed`) |
| `stopped` | warn | a [headless run](headless.md#when-the-run-ends) stopped: nothing runs, and nothing can be started |

- (1) `start` is 1 for the first start of the node, 2 for the first re-run, and so on.
- (2) warn when the process failed (it did not exit with a success exit code). The times are durations.

What you type into the terminals is never logged, and neither are the values of `envs`.

## The Log tab

The Log tab of the GUI shows the events (and the other messages about the node, e.g. when the connection to the
runner is lost) of the selected node and the nodes below it; for the root node, the messages about the tree too. It
follows the end of the log while it is scrolled to the bottom. The GUI keeps the last 100 000 messages.

## Saving the log

`runtree --log-db runs.db tree.rt.yaml` saves the events and the terminal output of the processes in an SQLite
database. Every runtree session is a new run in the same file, so it keeps the history of the runs. It is written in
the background (in batches, every 100 ms), and it can be read while runtree runs:

```
$ runtree --log-db runs.db --show-runs
RUN  STARTED              FINISHED             HOST  USER   EVENTS  OUTPUT   TREE
1    2026-10-17 23:45:44  2026-10-17 23:52:10  box   admin  31      12.4 KB  /home/admin/trees/upgrade.rt.yaml
$ runtree --log-db runs.db --show-events
2026-10-17 23:45:44 INFO  scheduler started
2026-10-17 23:45:44 INFO  [n2] upgrade › host7: starting on ssh://root@host7: apt-get -y upgrade
2026-10-17 23:47:12 WARN  [n2] upgrade › host7: exited with code 100 after 1m28s
...
$ runtree --log-db runs.db --show-output host7 | less -R
```

- `--show-events` and `--show-output` show the last run, `--run ID` selects another one.
- `--show-output` takes the id of the node (in brackets in the events), its title or its path, and prints the raw
  terminal output (with the escape sequences: `less -R` or a terminal shows them). When the node was run more than
  once, it shows the last start; `--start N` selects another one.
- The output is what the terminal showed, so it also has what the programs echoed of your input (but not passwords,
  programs do not echo them).
- The run is marked finished when runtree exits, also on SIGTERM. If runtree is killed, the last 0.1 s of records may
  be lost, and the run stays unfinished.
- The events are never waited for (they are logged while the tree is locked): if the disk is far behind, they are
  dropped, and the run list shows how many. The terminal output is never dropped, it slows down instead.

Several runtree processes can save to the same database at the same time (each one is a run), when the file is on a
local filesystem (SQLite's WAL mode does not work on network filesystems).

### The tables

The times are UTC; `datetime(time, 'localtime')` converts them.

| Table | Columns |
|-------|---------|
| `runs` | `id`, `started`, `finished` (empty while runtree runs, or when it was killed), `tree_file`, `host`, `user`, `pid`, `version`, `dropped` (the number of events that were not saved because the disk was too slow) |
| `nodes` | the nodes of each run: `run_id`, `node_id`, `parent_id`, `type`, `label` (the title or the command), `path` |
| `events` | `run_id`, `time`, `level`, `node_id`, `event`, `message`, and `attrs`: the other attributes as a JSON object (durations are in seconds) |
| `output` | `run_id`, `node_id`, `start` (1, then 2 for a re-run...), `time`, `data`: chunks of the raw terminal output |

For example:

```sql
-- the nodes that failed in the last run
SELECT n.path, e.time FROM events e JOIN nodes n USING (run_id, node_id)
WHERE e.run_id = (SELECT max(id) FROM runs) AND e.event = 'status' AND e.attrs ->> 'to' = 'Failed';

-- the slowest processes of all runs
SELECT e.run_id, n.path, e.attrs ->> 'elapsed' AS seconds FROM events e JOIN nodes n USING (run_id, node_id)
WHERE e.event = 'exited' ORDER BY seconds DESC LIMIT 10;
```
