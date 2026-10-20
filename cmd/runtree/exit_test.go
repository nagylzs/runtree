package main

import (
	"testing"

	"github.com/nagylzs/runtree/internal/rt"
)

func TestFinishExitCode(t *testing.T) {
	for st, want := range map[rt.Status]int{
		rt.StatusSuccess: 0, rt.StatusSkipped: 0, rt.StatusFailed: exitFailed, rt.StatusRunning: exitUnfinished,
		rt.StatusPaused: exitUnfinished, rt.StatusWaiting: exitUnfinished, rt.StatusFrozen: exitUnfinished,
	} {
		if got := finishExitCode(st); got != want {
			t.Errorf("%s: %d, want %d", rt.StatusName(st), got, want)
		}
	}
}
