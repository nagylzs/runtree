package version

import "testing"

func TestString(t *testing.T) {
	saved := []string{Version, Commit, Branch, Built}
	t.Cleanup(func() { Version, Commit, Branch, Built = saved[0], saved[1], saved[2], saved[3] })

	Version, Commit, Branch, Built = "v0.1.0", "1be6ff0e93e1a2b3c4d5e6f708192a3b4c5d6e7f", "main", "2026-10-21T17:12:09+02:00"
	if s, want := String(), "v0.1.0 (commit 1be6ff0e93e1, main, built 2026-10-21T17:12:09+02:00)"; s != want {
		t.Errorf("got %q, want %q", s, want)
	}
	if bt := BuiltTime(); bt == nil || bt.Hour() != 17 {
		t.Errorf("the build time is %v", bt)
	}
	Version, Commit, Branch, Built = "devel", "", "", ""
	if s := String(); s != "devel" {
		t.Errorf("without details: %q", s)
	}
	if BuiltTime() != nil {
		t.Error("an unknown build time is known")
	}
}

// A test binary has no version from its module
func TestFillFromBuildInfo(t *testing.T) {
	saved := Version
	t.Cleanup(func() { Version = saved })
	Version = ""
	fillFromBuildInfo()
	if Version == "" {
		t.Error("no version")
	}
}
