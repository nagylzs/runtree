// runtree-lsp is a language server for runtree tree files (for editors, e.g. the VS Code extension in
// editors/vscode). It communicates on stdin and stdout.
package main

import (
	"fmt"
	"os"

	"github.com/nagylzs/runtree/internal/lsp"
	"github.com/nagylzs/runtree/internal/version"
)

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--version", "-V":
			version.PrintVersion("runtree-lsp")
			return
		case "--stdio":
			// the default (some clients give it)
		case "-h", "--help":
			fmt.Println("runtree-lsp: language server for runtree tree files, on stdin and stdout")
			fmt.Println("  --version  print the version")
			return
		default:
			fmt.Fprintf(os.Stderr, "runtree-lsp: unknown argument %q\n", arg)
			os.Exit(2)
		}
	}
	os.Exit(lsp.Main())
}
