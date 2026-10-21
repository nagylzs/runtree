package runner

import (
	"strings"
	"testing"
)

// The OpenSSH of Windows has no ControlMaster: the connections are not shared, also when it is enabled
func TestNoSSHShareOnWindows(t *testing.T) {
	t.Cleanup(func() { SetSSHShare(false) })
	SetSSHShare(true)
	a, _ := ParseAddress("ssh://root@server01:2222")
	if got := strings.Join(sshOptions(a), " "); got != "-T -o BatchMode=yes -p 2222" {
		t.Errorf("the options are %s", got)
	}
}
