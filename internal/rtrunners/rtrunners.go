// Package rtrunners embeds the rtrunner binaries for the hosts of the managed ssh runners (rtrunner-<goos>-<goarch>.gz)
// in runtree and runtree-headless, when they are built with -tags rtrunners: scripts/build.py builds the binaries into
// internal/rtrunners/bin first (see embed.go). Without the tag, nothing is embedded.
package rtrunners
