package gtkui

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// recoverPanic must be deferred in gtk signal handlers that call into the tree. A panic would terminate runtree,
// and then the runners would terminate all running processes. Instead, the error is logged with its stack trace,
// and it is shown to the user. Locks must be released with defer in the handler, so that they are released before
// this runs.
func recoverPanic(what string) {
	if r := recover(); r != nil {
		reportPanic(what, r)
	}
}

// reportPanic logs a recovered panic with its stack trace, and shows it to the user.
func reportPanic(what string, r any) {
	slog.Error("internal error", "in", what, "error", r, "stack", string(debug.Stack()))
	ShowToast(fmt.Sprintf("Internal error in %s: %v (see the log for details)", what, r))
}
