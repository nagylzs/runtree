#include <glib-unix.h>
#include "_cgo_export.h"

// runtreeWatchMainQueue adds a source to the default main context, that calls runtreeMainQueueReady (mainqueue.go)
// when fd is readable. Its priority is the priority of the idle sources (glib.IdleAdd).
guint runtreeWatchMainQueue(int fd) {
	return g_unix_fd_add_full(G_PRIORITY_DEFAULT_IDLE, fd, G_IO_IN, runtreeMainQueueReady, NULL, NULL);
}
