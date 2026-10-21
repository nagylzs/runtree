package rt

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/nagylzs/runtree/internal/runner"
)

// windowsStatusNames are the names of some status codes of Windows, that a process exits with
var windowsStatusNames = map[uint32]string{
	0xC000013A: "Ctrl+C",
	0xC0000005: "access violation",
	0xC00000FD: "stack overflow",
	0xC0000409: "stack buffer overrun",
	0xC0000135: "a DLL was not found",
	0xC0000142: "a DLL could not be initialized",
	0xC000001D: "illegal instruction",
	0xC0000374: "heap corruption",
}

// ExitCodeText is an exit code for messages: a Windows status code (0x40000000 and above, Unix exit codes are below
// 256) is in hex, with its name when it is known, e.g. "0xC000013A (Ctrl+C)" instead of 3221225786
func ExitCodeText(code int) string {
	if code < 0x40000000 || code > 0xFFFFFFFF {
		return strconv.Itoa(code)
	}
	s := fmt.Sprintf("0x%08X", code)
	if name, ok := windowsStatusNames[uint32(code)]; ok {
		s += " (" + name + ")"
	}
	return s
}

// WindowsCommandLine is the command line of the arguments as Windows gets it (CreateProcess, the pseudo console of the
// Windows runner): the arguments with spaces are quoted, the quotes in them escaped (like syscall.EscapeArg)
func WindowsCommandLine(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = windowsEscapeArg(a)
	}
	return strings.Join(quoted, " ")
}

// windowsEscapeArg quotes an argument for CommandLineToArgvW: a backslash is special only before a quote
func windowsEscapeArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, "\" \t\\") {
		return s
	}
	hasSpace := strings.ContainsAny(s, " \t")
	var b strings.Builder
	if hasSpace {
		b.WriteByte('"')
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			slashes++
		case '"':
			for ; slashes > 0; slashes-- {
				b.WriteByte('\\')
			}
			b.WriteByte('\\')
		default:
			slashes = 0
		}
		b.WriteByte(c)
	}
	if hasSpace {
		for ; slashes > 0; slashes-- {
			b.WriteByte('\\')
		}
		b.WriteByte('"')
	}
	return b.String()
}

// RunnerOnWindows tells if the process of the node runs on Windows: what its runner told (runner.Client.RunnerSystem),
// or before it connected, what its address suggests: the local runner of runtree on Windows, or an ssh runner with
// a path on a drive (C:/...) or of an .exe. A TCP runner is not known until it connects.
func (n *Node) RunnerOnWindows(locked bool) bool {
	if !locked {
		panic("must be called in locked state")
	}
	if c := n.Client; c != nil {
		if s := c.RunnerSystem(); s != "" {
			return s == "windows"
		}
	}
	a, err := runner.ParseAddress(n.Calculated.Runner)
	if err != nil {
		return false
	}
	switch a.Network {
	case runner.NetworkLocal:
		return runtime.GOOS == "windows"
	case runner.NetworkSSH:
		p := strings.ToLower(a.Path)
		return len(p) >= 2 && p[1] == ':' && p[0] >= 'a' && p[0] <= 'z' || strings.HasSuffix(p, ".exe")
	}
	return false
}

// commandLine is the command line of the node for messages: quoted for a shell, or for Windows when it runs there
func (n *Node) commandLine(locked bool) string {
	if n.RunnerOnWindows(locked) {
		return WindowsCommandLine(n.Calculated.Args)
	}
	return quoteArgs(n.Calculated.Args)
}
