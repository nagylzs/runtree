# Runners

A runner starts the processes of the run nodes: it gives each one a pseudo-terminal, streams the terminal to the GUI,
passes the signals, and reports the exit status. The `runner` property of a node chooses where its processes run:

| `runner:` | Where the processes run |
|-----------|-------------------------|
| `local` (the default) | on the machine of `runtree`: the runner is built into it |
| `ssh://[user@]host[:port]` | on the host: ssh starts an `rtrunner` there for each process (runtree uploads it) |
| `ssh://[user@]host[:port]/path/to/rtrunner` | the same, with an `rtrunner` that is installed on the host |
| `host:port`, `tcp://host:port` | on the machine of an `rtrunner` daemon (`rtrunner -l :5000`) |

`runner` is inherited, and it can use variables:

```yaml
upgrade_host:
  runner: "ssh://root@{host}"
  seq:
    - [apt-get, update]
    - [apt-get, -y, dist-upgrade]
    - title: "wait until the host answers ping"
      runner: local                    # this one runs here
      args: [sh, -c, "until ping -c 1 {host}; do sleep 1; done"]
```

## The default runner

The nodes that do not specify a runner (and their parents do not either) use the local runner, or the one given to
`runtree`:

```bash
runtree --runner ssh://deploy@test-vm upgrade.rt.yaml
```

runs those nodes on `test-vm`, without changing the tree file. The nodes that must run on the machine of runtree (e.g.
the ones that wait for a host with `ping`) should say `runner: local`.

## The local runner

The local runner is built into `runtree`: nothing else needs to be started. The processes run on the machine of
runtree, as the user who started it, with the environment of runtree (see [environment](variables.md#environment)).

To run commands on other machines with the local runner, the commands can run `ssh` themselves, e.g. with
`args_prefix: [ssh, -t, "root@{host}"]`. This works anywhere, but the `envs` and `cwd` of the node apply to the local
`ssh` process, the signals of the GUI reach `ssh`, not the remote process, and a failed login is exit code 255, like
any other failure of the command. An ssh runner does not have these problems.

## ssh runners

With `runner: ssh://[user@]host[:port]`, runtree starts `ssh -T -o BatchMode=yes user@host` for each process, and
runs `rtrunner --stdio` on the host through it. The process runs on the host, in a terminal of `rtrunner`:

- its `envs` and `cwd` apply on the host (the default `cwd` is the home directory of the user);
- the signals of the GUI reach the process;
- its exit code and CPU times are the ones of the process, not of `ssh`;
- a failed login, or a missing `rtrunner`, is a start error of the node, with the message of ssh, e.g.
  `ssh web01: Permission denied (publickey).`

`ssh` is the ssh client of the system: it uses `~/.ssh/config` (host aliases, ports, `ProxyJump`...), the keys and
`ssh-agent`. It runs in the background, without a terminal, so it cannot ask for passwords or passphrases
(`BatchMode`), and the host key must be known already: connect to the host with `ssh` once.

### Managed rtrunner

Without a path (`ssh://root@host`), runtree manages `rtrunner` on the host:

- It starts `~/.cache/runtree/rtrunner-<sha256>` for the platform of the host (from `uname`), and when it is not
  there, it uploads it first, then starts it.
- The file is named by its hash: a new runtree build uploads its own one, and there is nothing to compare on the
  host. The upload is checked: the file gets its name only after the uploaded binary checked its own hash, so it is
  never incomplete.
- Nodes that start at the same time on the same host wait for one upload. An upload is a `runner-upload` event.
- The binaries that were not used for 30 days are deleted after an upload.
- The host needs `sh`, `uname`, `cat`, `mkdir`, `chmod`, `mv`, `touch`, `rm` and `find`: they are on every Linux
  and macOS system.

The binaries are in `runtree` when it is built with `scripts/build.py`: Linux on x86_64 (amd64), arm64, and 32 bit
ARM (e.g. a Raspberry Pi), and macOS on x86_64 and arm64 (not tested on a Mac yet). They are in `dist/rtrunners/` too,
for manual installs. `--rtrunner-dir DIR` (or the `RUNTREE_RTRUNNER_DIR` environment variable) is a directory of
`rtrunner-<goos>-<goarch>` files (e.g. `rtrunner-linux-amd64`) that are uploaded instead of the embedded ones; it can
add `linux-386` too. A runtree that is built with `go build` has no binaries to upload: use a path then.

### With a path

With a path (`ssh://root@host/usr/local/bin/rtrunner`, or `ssh://root@host/~/bin/rtrunner` in the home directory),
that `rtrunner` is started, and nothing is uploaded. It must be the same protocol version as runtree (a different
version is a start error), so install the `rtrunner` that was built with your runtree. A path is needed for hosts
whose platform runtree has no binary for, and for Windows hosts (not tested).

### Sharing the connection

The processes on the same host (with the same user and port) share one ssh connection (OpenSSH `ControlMaster`), so
only the first one logs in, and the others start fast. The connection stays open for a minute after its last use (also
after runtree exits). Above the `MaxSessions` limit of the ssh server (10 by default), ssh opens another connection.

The sockets of the connections are in `$XDG_RUNTIME_DIR/runtree`, or in the cache directory of the user when it is not
set (e.g. `~/.cache/runtree/ssh`, `~/Library/Caches/runtree/ssh` on macOS). `--no-ssh-share` turns the sharing off.

## rtrunner daemons

`rtrunner` can also run as a daemon on a machine:

```bash
rtrunner -v -l :5000
```

and the nodes use it with `runner: host:5000` (or `tcp://host:5000`). The processes run on that machine, as the user
of `rtrunner`.

The connection is neither authenticated nor encrypted: anyone who can connect to the port can run commands as the user
of `rtrunner`. Use it on trusted networks only (e.g. listen on `127.0.0.1` and reach it through an ssh tunnel), or use
an ssh runner instead.

## Starting and stopping processes

A node connects to its runner in the background: the GUI shows "Running, connecting" until the process has started,
so a slow login does not hold up the scheduler or the GUI, and the nodes of a `par` node connect at the same time.

When runtree exits (the window is closed, or it gets SIGTERM), it terminates the processes that still run: SIGINT, then
SIGTERM after 10 seconds, and SIGKILL after 10 more seconds, and it waits until they exit, so that their exits are
logged. The local runner does it itself; the processes of ssh and TCP runners get the signals through their
connections, and the `rtrunner` terminates the rest of their process group when runtree disconnects. If runtree is
killed (SIGKILL), the processes of the local runner get SIGHUP when their terminals are closed, and the `rtrunner`s
terminate theirs when the connection is gone.

The processes do not survive runtree: there is no way to reattach to them from a new runtree yet.
