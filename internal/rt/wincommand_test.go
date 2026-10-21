package rt

import "testing"

func TestExitCodeText(t *testing.T) {
	for code, want := range map[int]string{
		0:          "0",
		3:          "3",
		255:        "255",
		9009:       "9009",
		3221225786: "0xC000013A (Ctrl+C)",
		3221225477: "0xC0000005 (access violation)",
		3221225794: "0xC0000142 (a DLL could not be initialized)",
		0xC0000001: "0xC0000001",
		-1:         "-1",
	} {
		if got := ExitCodeText(code); got != want {
			t.Errorf("%d: got %q, want %q", code, got, want)
		}
	}
}

// The command line as Windows gets it (the same as syscall.EscapeArg on Windows)
func TestWindowsCommandLine(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"cmd", "/c", "echo hello & ver"}, `cmd /c "echo hello & ver"`},
		{[]string{"cmd", "/c", `dir C:\Windows\System32`}, `cmd /c "dir C:\Windows\System32"`},
		{[]string{`C:\Program Files\x\a.exe`, ""}, `"C:\Program Files\x\a.exe" ""`},
		{[]string{"robocopy", `C:\src\`, `C:\dst dir\`}, `robocopy C:\src\ "C:\dst dir\\"`},
		{[]string{"powershell", "-Command", `Write-Host "hi there"`}, `powershell -Command "Write-Host \"hi there\""`},
		{[]string{"echo", `a"b`}, `echo a\"b`},
		{[]string{"echo", `a\"b`}, `echo a\\\"b`},
	} {
		if got := WindowsCommandLine(tc.args); got != tc.want {
			t.Errorf("%q: got %s, want %s", tc.args, got, tc.want)
		}
	}
}

// Before the runner tells its system, the address suggests it
func TestRunnerOnWindows(t *testing.T) {
	for runnerAddress, want := range map[string]bool{
		"ssh://nagyl@htpc/C:/runtree/rtrunner.exe": true,
		"ssh://htpc/c:/rt":                         true,
		"ssh://htpc/opt/rtrunner.exe":              true,
		"ssh://deploy@web01":                       false,
		"ssh://web01/usr/local/bin/rtrunner":       false,
		"tcp://htpc:5000":                          false, // not known before it connects
	} {
		n := &Node{Type: TypeRun}
		n.Calculated.Runner = runnerAddress
		if got := n.RunnerOnWindows(true); got != want {
			t.Errorf("%s: %v", runnerAddress, got)
		}
	}
}
