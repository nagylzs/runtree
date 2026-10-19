package runner

import (
	"os"
	"slices"
	"strings"
	"sync"
)

var (
	hiddenEnvLock sync.Mutex
	hiddenEnv     = map[string]bool{}
)

// HideEnv keeps an environment variable of this process from the processes that the local runner starts: one that
// runtree sets for itself (e.g. the GTK renderer of the GUI), not for the commands of the nodes. rtrunner is a
// process of its own, it is not affected.
func HideEnv(name string) {
	hiddenEnvLock.Lock()
	defer hiddenEnvLock.Unlock()
	hiddenEnv[name] = true
}

// environ is the environment of this process without the hidden variables (see HideEnv)
func environ() []string {
	hiddenEnvLock.Lock()
	defer hiddenEnvLock.Unlock()
	env := os.Environ()
	if len(hiddenEnv) == 0 {
		return env
	}
	return slices.DeleteFunc(env, func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return hiddenEnv[name]
	})
}
