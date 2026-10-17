# runtree for VS Code

Editor support for [runtree](https://github.com/nagylzs/runtree) tree files. Tree files must be named `*.rt.yaml`
(or `*.rt.yml`).

## Features

- Errors and warnings of the tree files as you type, from the runtree language server (`runtree-lsp`): unknown
  properties, invalid values, includes, undefined variables, and the other checks of `runtree --check`. The files
  that a tree includes are checked as part of the tree (with the include chain), also when they are not open.
- Go to definition (F12) on an `include` argument opens the included object.
- Hover on a `{variable}` shows its values in the nodes of the line (for each `for_vars` combination, or each place
  where the object is included), and where each value is set (`vars`, `def_vars` or `for_vars`, with a link).
- Completion of `include` arguments: the objects of the file, and `file|name` for the objects of the other runtree
  files.
- Completion of node properties and their values, with documentation on hover.
- Validation of property names (typos like `on_eror`), value types and allowed values (e.g. `on_error`, `status`).
- The `!raw` tag of runtree (strings without variable substitution) is known to the YAML extension (the
  `yaml.customTags` setting; if you set it yourself, add `"!raw scalar"` to your list).
- A problem matcher (`$runtree`) for `runtree --check`, which also checks includes, variables and the other
  semantics of the tree, and reports all of its errors and warnings (e.g. undefined variables) in the Problems panel.

The completion and the validation are provided by the [YAML extension](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml)
of Red Hat (it is installed with this extension), with the JSON Schema of runtree tree files
(`schema/runtree.schema.json` in the repository, generated from the node property table of runtree).

## Requirements

`runtree-lsp` on the `PATH` (`go install github.com/nagylzs/runtree/cmd/runtree-lsp@latest`), or its path in the
`runtree.languageServer.path` setting. Without it, the extension still gives the schema based completion and
validation; `runtree.languageServer.enabled` turns the language server off. *runtree: Restart the language server* in
the command palette restarts it.

The language server checks the YAML files that have a top level `tree` object, the files that they include, and the
open `*.rt.yaml` files. The completion and the schema validation are only for `*.rt.yaml` files.

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
npm install
npm run package                        # creates runtree-<version>.vsix
code --install-extension runtree-*.vsix
```

Packaging copies the schema and the license from the repository root.
