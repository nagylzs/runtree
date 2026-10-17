package rt

import (
	"fmt"
	"runtime/debug"
)

// recoverPanic must be deferred in the goroutines of the tree (the scheduler and the runner callbacks). A panic
// would terminate runtree, and then the runners would terminate all running processes. Instead, the error is logged
// with its stack trace, and it is shown to the user. Locks must be released with defer, so that they are released
// before this runs.
func (t *Tree) recoverPanic(what string) {
	r := recover()
	if r == nil {
		return
	}
	t.logger.Error("internal error", "in", what, "error", r, "stack", string(debug.Stack()))
	if t.terminalHelper != nil {
		t.terminalHelper.OnInternalError(fmt.Sprintf("Internal error in %s: %v (see the log for details)", what, r))
	}
}
