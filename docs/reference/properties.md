# Node properties

<!-- Generated from NodeProperties (internal/rt/properties.go), do not edit it. Regenerate it with:
     go test ./internal/rt/ -run TestPropertiesDocIsUpToDate -update-docs -->

The properties of the nodes in tree files. The guide explains how they work together: [writing tree
files](../tree-files.md), [variables](../variables.md), [how a tree runs](../execution.md).

An **inherited** property that is not set in a node gets the value of the parent node. Variables (`{name}`) can be used in the strings of `title`, `description`, `args`, `args_prefix`, `cwd`, `envs`, `runner`, `xlocks`, `rlocks`, `provides`, `requires`, `ifeq`, `ifneq`, `for_vars`, `vars` and `def_vars`.

| Property | Value | Inherited | Default |
|----------|-------|:---------:|---------|
| [`type`](#type) | `run`, `seq` or `par` |  | `run` |
| [`id`](#id) | string |  |  |
| [`title`](#title) | string |  |  |
| [`description`](#description) | string |  |  |
| [`args`](#args) | list of strings |  |  |
| [`args_prefix`](#args_prefix) | list of strings | yes |  |
| [`cwd`](#cwd) | string | yes |  |
| [`runner`](#runner) | string | yes | `local` |
| [`vars`](#vars) | object of strings or lists of strings | yes |  |
| [`def_vars`](#def_vars) | object of strings or lists of strings |  |  |
| [`inherit_vars`](#inherit_vars) | boolean |  | `true` |
| [`envs`](#envs) | object of strings | yes |  |
| [`system_envs`](#system_envs) | boolean |  | `true` |
| [`inherit_envs`](#inherit_envs) | boolean |  | `true` |
| [`xlocks`](#xlocks) | list of strings |  |  |
| [`rlocks`](#rlocks) | list of strings | yes |  |
| [`provides`](#provides) | list of strings |  |  |
| [`requires`](#requires) | list of strings |  |  |
| [`max_proc`](#max_proc) | integer | yes | `-1` |
| [`success_exit_codes`](#success_exit_codes) | list of integers (0-255) | yes | `[0]` |
| [`timeout`](#timeout) | duration (e.g. 30m), or 0 | yes | `0` |
| [`status`](#status) | `waiting` or `frozen` | yes | `waiting` |
| [`on_error`](#on_error) | status and/or sibling operation | yes | `failed` |
| [`children_on_error`](#children_on_error) | status and/or sibling operation |  |  |
| [`expanded`](#expanded) | boolean |  | `false` |
| [`expand_on_active`](#expand_on_active) | boolean |  | `true` |
| [`collapse_on_finished`](#collapse_on_finished) | boolean |  | `true` |
| [`nodes`](#nodes) | list of nodes |  |  |
| [`seq`](#seq) | list of nodes |  |  |
| [`par`](#par) | list of nodes |  |  |
| [`include`](#include) | string or list of strings |  |  |
| [`for_vars`](#for_vars) | object of lists of strings (or list variable names) |  |  |
| [`ifeq`](#ifeq) | object of strings or lists of strings |  |  |
| [`ifneq`](#ifneq) | object of strings or lists of strings |  |  |

## type

The type of the node: run (runs a command), seq (runs its sub-nodes one after the other) or par (runs its sub-nodes in parallel). It is not needed when the node has a seq or par property, and a node with include is a seq node by default.

Value: `run`, `seq` or `par`, default: `run`. See [the guide](../tree-files.md#nodes).

## id

Identifier of the node, it must match [a-zA-Z][a-zA-Z0-9_-]* and be unique in the tree. A generated id is used when it is not given.

Value: string. See [the guide](../tree-files.md#nodes).

## title

A single line, human-readable title, displayed in the GUI. Variables can be used.

Value: string. See [the guide](../tree-files.md#nodes).

## description

Multi-line description of the node, displayed in the GUI when the node is selected.

Value: string. See [the guide](../tree-files.md#nodes).

## args

The command and its arguments of a run node (after args_prefix). They are not given to a shell: the first one is looked up in the PATH of the runner. It is not inherited. Variables can be used.

Value: list of strings. See [the guide](../tree-files.md#run-nodes).

## args_prefix

Arguments that are put before the args of the run nodes in this subtree, e.g. ["ssh", "root@{server}"]. Its variables are the ones of the node that sets it.

Value: list of strings, inherited. See [the guide](../variables.md#command-prefix).

## cwd

The directory where the processes are started. When it is empty, the current directory of the runner is used (runtree's own for the local runner, the home directory for an ssh runner). ~ is not expanded, a cwd that starts with ~ is an error.

Value: string, inherited. See [the guide](../variables.md#working-directory).

## runner

The runner that runs the processes of the node: local (runtree runs them itself), the address (host:port or tcp://host:port) of an rtrunner, or ssh://[user@]host[:port][/path/to/rtrunner] (ssh starts rtrunner on the host for each process). When it is not set, it is inherited from the parent; the default of the root is local, or the --runner option of runtree.

Value: string, inherited, default: `local`. See [the guide](../runners.md).

## vars

Variables (name: value) for this node and its subtree. Values are strings or lists of strings, and can be used as {name} in other properties.

Value: object of strings or lists of strings, inherited. See [the guide](../variables.md#variables).

## def_vars

Default variable values: only used when the variable is not inherited from the parent. A variable cannot be in both vars and def_vars.

Value: object of strings or lists of strings. See [the guide](../variables.md#default-variables).

## inherit_vars

Inherit the variables of the parent node. When it is false, the node only has its own variables and the for_vars of its parent; def_vars cannot be used then.

Value: boolean, default: `true`. See [the guide](../variables.md#inheritance).

## envs

Environment variables (name: value) for the processes of this subtree. The values can use variables, and {NAME} of the inherited envs.

Value: object of strings, inherited. See [the guide](../variables.md#environment).

## system_envs

Use the environment of the runner (runtree's own for the local runner) as a base environment for the processes.

Value: boolean, default: `true`. See [the guide](../variables.md#environment).

## inherit_envs

Inherit the environment variables (envs) of the parent node.

Value: boolean, default: `true`. See [the guide](../variables.md#environment).

## xlocks

Names of resources that are locked exclusively while the node is running or paused: no other node can lock them meanwhile. A name cannot be in the rlocks of the node too (also inherited ones). It is not inherited.

Value: list of strings. See [the guide](../coordination.md#locks).

## rlocks

Names of resources that are locked while the node is running or paused. The topmost node that lists a name holds it, its subtree shares it, other nodes cannot lock it meanwhile. It is inherited, the node's own rlocks replace the inherited ones.

Value: list of strings, inherited. See [the guide](../coordination.md#locks).

## provides

Names of resources that are provided when the node finishes without an error (success, or skipped). The nodes that require them wait until then. It is not inherited.

Value: list of strings. See [the guide](../coordination.md#dependencies).

## requires

Names of resources that must be provided (by other nodes) before the node can be started. It is not inherited.

Value: list of strings. See [the guide](../coordination.md#dependencies).

## max_proc

Maximum number of running processes in this subtree; a negative value means no limit, 0 starts nothing. It is inherited: a sub-node limits its own subtree with the same number.

Value: integer, inherited, default: `-1`. See [the guide](../execution.md#limiting-processes).

## success_exit_codes

Process exit codes that mean success, e.g. [0, 255] for "ssh host reboot". The listed codes replace the default, so list 0 too if it should also mean success.

Value: list of integers (0-255), inherited, default: `[0]`. See [the guide](../execution.md#exit-codes).

## timeout

The longest time that a run node may run, e.g. 90s, 30m or 1h30m (0: no limit). Then its process is stopped (Ctrl+C is typed into its terminal, SIGTERM and SIGKILL follow if it does not stop), and the node fails: it gets the status of its on_error. It is inherited: on a seq or par node it is the limit of each run node below it, not of the group.

Value: duration (e.g. 30m), or 0, inherited, default: `0`. See [the guide](../execution.md#timeouts).

## status

The initial status of the node. A frozen node is not started until it is thawed manually. It is inherited: the sub-nodes of a frozen seq or par node are frozen too.

Value: `waiting` or `frozen`, inherited, default: `waiting`. See [the guide](../execution.md#frozen-nodes).

## on_error

What to do when the process or a sub-node fails: the status of the node (failed, paused or success) and/or an operation on the not yet started siblings (none, skip or freeze), e.g. paused, [paused, freeze] or "paused, freeze". The part that is not given is the default (failed, none). Inherited from the parent's children_on_error, or from its on_error.

Value: status and/or sibling operation, inherited, default: `failed`. See [the guide](../execution.md#errors).

## children_on_error

The on_error value that the sub-nodes inherit (when they do not specify on_error). When it is not given, the sub-nodes inherit the on_error of the node.

Value: status and/or sibling operation. See [the guide](../execution.md#errors).

## expanded

Display the sub-nodes of the node in the GUI when the tree is loaded.

Value: boolean, default: `false`. See [the guide](../gui.md#the-tree).

## expand_on_active

Expand the node in the GUI when it becomes active.

Value: boolean, default: `true`. See [the guide](../gui.md#the-tree).

## collapse_on_finished

Collapse the node in the GUI when it finishes.

Value: boolean, default: `true`. See [the guide](../gui.md#the-tree).

## nodes

The sub-nodes of a seq or par node.

Value: list of nodes. See [the guide](../tree-files.md#nodes).

## seq

Sub-nodes that run one after the other (this makes the node a seq node).

Value: list of nodes. See [the guide](../tree-files.md#nodes).

## par

Sub-nodes that run in parallel (this makes the node a par node).

Value: list of nodes. See [the guide](../tree-files.md#nodes).

## include

Top level objects to load as sub-nodes, from this file (name) or from another file (file|name, relative to this file). A node that includes one object (without for_vars and locks) becomes that object, with its own title, description, provides and requires.

Value: string or list of strings. See [the guide](../tree-files.md#include).

## for_vars

Repeat the sub-nodes for every combination of the listed variable values (lists of strings, or names of list variables). The first variable changes the slowest.

Value: object of lists of strings (or list variable names). See [the guide](../tree-files.md#for_vars).

## ifeq

Include the node only if all of the given variables have the given values (an undefined variable is empty).

Value: object of strings or lists of strings. See [the guide](../tree-files.md#conditions).

## ifneq

Include the node only if all of the given variables differ from the given values (an undefined variable is empty).

Value: object of strings or lists of strings. See [the guide](../tree-files.md#conditions).
