# runtree for VS Code

Editor support for [runtree](https://github.com/nagylzs/runtree) tree files. Tree files must be named `*.rt.yaml`
(or `*.rt.yml`).

## Features

- Completion of node properties and their values, with documentation on hover.
- Validation of property names (typos like `on_eror`), value types and allowed values (e.g. `on_error`, `status`).
- A problem matcher (`$runtree`) for `runtree --check`, which also checks includes, variables and the other
  semantics of the tree, and reports its errors in the Problems panel.

The completion and the validation are provided by the [YAML extension](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml)
of Red Hat (it is installed with this extension), with the JSON Schema of runtree tree files
(`schema/runtree.schema.json` in the repository, generated from the node property table of runtree).

## Checking a tree with runtree

Add a task to `.vscode/tasks.json`, then run it with *Terminal › Run Task*:

```json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "runtree: check",
      "type": "shell",
      "command": "runtree --check ${file}",
      "problemMatcher": "$runtree"
    }
  ]
}
```

## Building

```bash
cd editors/vscode
npm run package                        # creates runtree-<version>.vsix
code --install-extension runtree-*.vsix
```

Packaging copies the schema and the license from the repository root.
