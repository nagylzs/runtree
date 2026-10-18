//go:build rtrunners

package rtrunners

import (
	"embed"
	"io/fs"

	"github.com/nagylzs/runtree/internal/runner"
)

//go:embed bin/rtrunner-*.gz
var rtrunners embed.FS

func init() {
	fsys, err := fs.Sub(rtrunners, "bin")
	if err != nil {
		panic(err)
	}
	runner.SetEmbeddedBinaries(fsys)
}
