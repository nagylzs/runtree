# Editor support

## VS Code

The extension in [`editors/vscode`](../editors/vscode/README.md) gives:

- the errors and warnings of `runtree --check` as you type (from the language server, also for the included files);
- go to definition on `include` arguments, the values of a `{variable}` on hover, completion of `include` arguments;
- completion, documentation and validation of the node properties and their values (from the JSON Schema);
- the `!raw` tag, and a problem matcher (`$runtree`) for `runtree --check` tasks.

It is not published yet: build it with `npm install && npm run package` in `editors/vscode`, and install the `.vsix`
file (*Extensions › ... › Install from VSIX*). Its [README](../editors/vscode/README.md) has the details.

## The JSON Schema

`schema/runtree.schema.json` is a JSON Schema of tree files (`runtree --schema` prints the same). It is generated from
the node property table of runtree, so it always matches the parser. With the YAML extension of Red Hat (in VS Code,
and in other editors that use `yaml-language-server`), it gives completion, documentation on hover, and validation of
the property names and values. Associate it with your tree files in the settings:

```json
"yaml.schemas": {
  "https://raw.githubusercontent.com/nagylzs/runtree/main/schema/runtree.schema.json": ["*.rt.yaml"]
}
```

or put this comment at the top of a tree file:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/nagylzs/runtree/main/schema/runtree.schema.json
```

The schema only checks the structure; `runtree --check` also checks includes, variables and the other semantics. The
YAML extension reports the `!raw` tag as unknown, unless it is listed in the settings (the runtree extension does
this):

```json
"yaml.customTags": ["!raw scalar"]
```

## The language server

`runtree-lsp` is a language server (LSP, on stdin and stdout) for tree files, for any editor that supports language
servers:

- the errors and warnings of `runtree --check` as you type, with the unsaved changes, and for the included files too;
- go to definition for `include` arguments;
- the values of a `{variable}` on hover, in every node that the line belongs to (e.g. for each `for_vars`
  combination, or each place where the object is included), with where the value is set;
- completion of `include` arguments (the objects of the file, and `file|name` for the other runtree files).

It checks the YAML files of the workspace that have a top level `tree` object, the files that they include, and the
open `*.rt.yaml` files. A `*.rt.yaml` file that no tree includes is checked object by object, without the undefined
variable warnings (the variables are set where the objects are included).

It does not need GTK, so it builds on Linux, Windows and macOS:

```bash
go install github.com/nagylzs/runtree/cmd/runtree-lsp@latest
```

`scripts/build.py` builds it too.
