
op.go PerformOp
run.go start

* implement "if" node type
* implement NavBlocked navmode
* document "include" property, explain include/reduce
* show requires/provides on the dependencies tab
* populate node logs, it should be structured
* possibly show a progress bar for each node, that shows the percentage of finished nodes
* create a special view that shows tree total time, tree total progress and processing speed graph?
* MAYBE after selecting a status filter in the navbar, show a graph for number of nodes for that status?
* show provides and requires under dependencies
* play a sound when a node goes paused
* create a main menu, and add menu items to toggle the various windows above
* add extra actions for paused nodes to deal with siblings: cancel siblings, freeze siblings, thaw siblings, this is
  very low priority, you can do it on its parent...
* command line switch to exit app when the root node succeeds or fails
* display CWD in properties
* confirm GUI close when not all nodes are finished, and stop scheduler when GUI is closed

# Adoptability (from a review, 2026-09-16)

Unique combination worth keeping: seq/par tree + locks/deps, live interactive PTY per step,
pause-on-error -> manual fix -> resume automation, remote execution. No mainstream tool
(Ansible, CI DAGs, Rundeck/AWX, mprocs/overmind, tmux scripts) covers all four.

Ordered by impact:

* SSH transport for runners, instead of the unauthenticated TCP daemon on :5000
  (e.g. `ssh host rtrunner --stdio`, or use SSH's own pty). Removes the deploy step on
  target hosts and the main security objection.
* TUI frontend (non-GTK), so runtree works headless, over SSH, and on macOS/Windows.
  GTK4+VTE+cgo is the biggest install/build barrier.
* Headless mode: exit with root node's status (already listed above), so it can double as a CI step.
* Tests, at least for parser, vars substitution, scheduler, and lock/deps semantics.
* Reduce DSL friction: avoid duplicating inventory via per-node `runner`; make it easy for
  a leaf node to call an existing Ansible playbook / script.
* Pitch: "`make -j` for ops, where every job is a terminal you can step into".
