# The GUI

The window has two parts: the tree on the left, and the selected node on the right (the divider between them can be
dragged).

![runtree](images/runtree.png)

## The tree

Each row is a node: its title (or command line), its type (`Seq`, `Par`), its status, and its times:

- the status, with details: the number of running processes below it (`Running (2)`), the number of failed run nodes
  (`1 failed`), why it waits (`blocked`, `max proc reached`, `won't schedule`), `connecting` while a process is being
  started, and the pid of the process;
- the elapsed time; for seq and par nodes also the total time of the processes below it (`tot=`) and the speedup
  that the parallel runs gave (`×2.0`);
- a terminal icon for the run nodes that have a terminal.

The buttons above the tree are views: **Tree** shows the whole tree, the others (Waiting, Frozen, Running, Success,
Failed, Skipped, Paused) list the nodes in that status, with their numbers. A check box limits the lists to run nodes.
In a list, the location button of a node shows it in the tree: it switches to the Tree view, opens the parents of the
node, selects it, and scrolls to it.

Three properties control which nodes are shown opened: `expanded` (show the sub-nodes when the tree is loaded,
`false` by default), `expand_on_active` (open the node when it becomes active, `true` by default) and
`collapse_on_finished` (close it when it finishes, `true` by default).

When the tree files have [warnings](tree-files.md#checking-a-tree-file), a banner above the tree tells it; its
**Show warnings** button lists them.

## The selected node

The top of the right side shows the title and the path of the node, the operation buttons, and its status (its
tooltip explains the status). The nodes of the path are links: a click shows that node in the tree. Below them are
the tabs:

| Tab | Shows |
|-----|-------|
| Properties | the calculated properties: pid, exit code, start and finish times, elapsed times, runner, working directory (`cwd`), command line, `max_proc`, `on_error`, `success_exit_codes`... |
| Dependencies | what the node requires and provides, its locks, and what it waits for |
| Log | the [events](logging.md#events) of the node and the nodes below it (of the whole tree for the root). It follows the end of the log; when it is scrolled up, the **End** button in its corner scrolls to the end and follows it again |
| Description | the `description` of the node |
| Vars | the variables of the node, and where each value comes from |
| Envs | the environment variables that the node sets |

The values can be copied with the button next to them.

## Terminals

Every started run node has a terminal below the tabs: the terminal of its process. It is a real terminal: programs
can use colors, ask questions, show progress bars, and you can type into it.

- **Copy** and **Paste**: Ctrl+Shift+C and Ctrl+Shift+V, or the context menu (right click), which has **Select
  All** too. Selecting with the mouse and pasting with the middle button works as in other terminals.
- Pasting text with line breaks (with Ctrl+Shift+V or the menu) asks for confirmation first, with a preview: pasted
  into a shell, it can run commands. Pasting with the middle mouse button is not confirmed.
- The terminal stays when the process exits, so its output can be read later. The output can also be saved, see
  [Logs and history](logging.md#saving-the-log).

## Operations

The buttons of the selected node perform operations on it. A button is enabled when the operation is allowed in the
status of the node. For a seq or par node there is a second row: the same operations with **all**, that are performed
on every node of the subtree where they are allowed (e.g. **Freeze all** freezes the waiting run nodes below it).

| Operation | Allowed (run nodes) | What it does |
|-----------|---------------------|--------------|
| Freeze | Waiting | the scheduler does not start the node until it is thawed |
| Thaw | Frozen | the node is Waiting again |
| Run / Re-run | any status but Running | starts the node now: also a frozen, paused or finished one (Re-run), even when `max_proc` is reached or its requirements are missing; its locks must be free |
| Signal | Running | sends a signal (chosen from a list, e.g. SIGINT for Ctrl+C, SIGTERM, SIGKILL) to the process |
| Fail / Success | Paused | sets the final status of a paused node |
| Skip | Waiting, Frozen | the node is Skipped: finished without error, it is not started |
| ResetToWaiting | Frozen, Paused, finished | the node is Waiting again, the scheduler starts it again when it can |

For seq and par nodes:

- **Skip** for a node that is not started yet (Waiting or Frozen): the node is Skipped, its sub-nodes are not started.
  **Skip all** skips the sub-nodes too; on a running node it only skips the sub-nodes that are not started.
- **Thaw** and **ResetToWaiting** for a node frozen by `status: frozen`, **ResetToWaiting** for a skipped one: the
  status of the node is calculated from its sub-nodes again. A node that is frozen because its sub-nodes are frozen is
  thawed with **Thaw all**.
- **Fail** / **Success** for a paused one. This status lasts until an operation is performed on the node or below it.
- The "all" versions for the rest.

**Re-run** and **ResetToWaiting** unprovide the resources that the node (and its parents that become unfinished)
provided, so the nodes that require them wait for the new run. When there are such resources, a dialog lists them:
unprovide all of them (the default), or choose the ones to unprovide. A resource that is kept stays available, and the
nodes that require it can start right away. Resetting a node also clears the error of its previous start (e.g. when
its runner could not be reached).

## Closing runtree

When the tree has run nodes that are not finished successfully (not Success or Skipped), closing the window asks for
confirmation: to avoid closing it by accident, you have to type the result of a small addition. Ctrl+C in the terminal
that started runtree (SIGINT), and closing that terminal (SIGHUP), close the window the same way.

When runtree exits, it stops starting nodes, terminates the processes that still run (SIGINT, then SIGTERM and SIGKILL
after 10 seconds each), and waits for them, so that their exits are logged. The window closes right away, the
messages are printed on the terminal that started runtree. See [signals and exit](practice.md#signals-and-exit).

## Appearance

runtree is designed for dark GTK themes, e.g. `GTK_THEME=Adwaita:dark`. With light themes, some parts may still look
dark. On a HiDPI screen, `GDK_SCALE=2` doubles the size.
