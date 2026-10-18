//go:build rtrunners

package main

import (
	"embed"
	"io/fs"

	"github.com/nagylzs/runtree/internal/runner"
)

// The rtrunner binaries for the hosts of the managed ssh runners (rtrunner-<goos>-<goarch>.gz), scripts/build.py builds
// them into cmd/runtree/rtrunners, and builds runtree with -tags rtrunners.
//
//go:embed rtrunners/rtrunner-*.gz
var rtrunners embed.FS

func init() {
	fsys, err := fs.Sub(rtrunners, "rtrunners")
	if err != nil {
		panic(err)
	}
	runner.SetEmbeddedBinaries(fsys)
}
