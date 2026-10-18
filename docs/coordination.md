# Dependencies and locks

The structure of a tree says what runs in order and what runs in parallel. Dependencies and locks order the steps
across the branches of the tree, so the tree can be organized by what it does (e.g. per component, or per server),
and runtree still runs everything in parallel as much as possible.

## Dependencies

`provides` and `requires` list names of resources (any strings). A node that requires a resource is not started
until a node that provides it has finished without an error:

```yaml
tree:
  par:
    - title: "build"
      provides: [binary]
      args: [make, build]
    - title: "deploy"
      requires: [binary]
      args: [make, deploy]
```

- A node provides its resources when it finishes without an error: Success, or Skipped. A failed node does not
  provide.
- When the tree starts, nothing is provided. A resource stays provided until a node that provided it is
  [run again](gui.md#operations) or [reset to waiting](gui.md#operations): then it is **unprovided** (unless another
  node provides it too), and the nodes that require it wait for the new run. The GUI asks which resources to
  unprovide, so you can keep some of them.
- The names can use [variables](variables.md#variables), e.g. `provides: ["binary-{branch}"]`.
- `provides` and `requires` are not inherited. Under a seq or par node they still work as expected: the node is
  finished when its sub-nodes are, and the scheduler does not start anything below a node that is blocked.
- The names are independent of the lock names.

A tree can deadlock, e.g. when nothing provides a required resource, or two nodes require what the other one provides:
runtree does not detect it, the nodes simply wait. The Dependencies tab of the GUI shows what a node requires and
provides, and what it is waiting for.

## Locks

Some resources can only be used by one process (or one group of processes) at a time: a build server, a database
during a migration, the load balancer while a server leaves or joins the pool. Name them in `xlocks` or `rlocks`:

```yaml
tree:
  par:
    - {title: "build a", xlocks: [build-server], args: [make, a]}
    - {title: "build b", xlocks: [build-server], args: [make, b]}
```

- **xlocks** are exclusive: while a node holds the lock, no other node can lock the same name.
- **rlocks** are held by a subtree: the topmost node that lists the name holds it, and the nodes below it share it.
  Other nodes cannot lock the name (with `xlocks` or `rlocks`) meanwhile.

A node acquires all of its locks at once when it becomes Running, and it releases them when it gets another status
(a paused node keeps them). A node that cannot acquire its locks is [blocked](execution.md#blocking): the scheduler
waits until the locks are released. Starting a node by hand (Run) also needs its locks.

A seq or par node with locks holds them from the moment its first sub-node becomes active until all of its sub-nodes
are finished. So a lock on a seq or par node covers a group of steps:

```yaml
tree:
  par:
    - title: "maintenance window of db01"
      rlocks: [db01]
      seq:
        - [systemctl, stop, app]
        - [./migrate-database]
        - [systemctl, start, app]
    - title: "backup of db01"
      xlocks: [db01]          # waits until the whole maintenance window has finished
      args: [./backup, db01]
```

- Lock names can use variables, e.g. `rlocks: ["{host}"]`.
- `xlocks` are not inherited. `rlocks` are inherited (the sub-nodes share the lock of their parent); a node's own
  `rlocks` replace the inherited ones.
- A name cannot be in both the `xlocks` and the `rlocks` of a node, also not in an inherited `rlocks`: the node would
  wait for the lock of its own group. It is an error when the tree is loaded.
- Locks can deadlock too (two nodes that wait for the lock that the other one holds): runtree does not detect it.

The [locks example](../examples/08-locks.rt.yaml) shows both kinds.
