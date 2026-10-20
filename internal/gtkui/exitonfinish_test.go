package gtkui

import "testing"

// The countdown starts when the root node is finished, it exits exitDelay seconds later; it stops when the root is
// started again (and starts again when it finishes again), and never exits after Stay open
func TestCountdown(t *testing.T) {
	var c countdown
	if left, exit := c.tick(false); left != -1 || exit {
		t.Errorf("before the end: %d %v", left, exit)
	}
	for i := exitDelay; i > 1; i-- {
		if left, exit := c.tick(true); left != i || exit {
			t.Fatalf("%d seconds left: %d %v", i, left, exit)
		}
	}
	if left, exit := c.tick(false); left != -1 || exit { // Re-run
		t.Errorf("after the re-run: %d %v", left, exit)
	}
	for i := exitDelay; i > 0; i-- {
		if left, _ := c.tick(true); left != i {
			t.Fatalf("again, %d seconds left: %d", i, left)
		}
	}
	if left, exit := c.tick(true); left != 0 || !exit {
		t.Errorf("at the end: %d %v", left, exit)
	}

	c = countdown{}
	c.tick(true)
	c.stay = true
	for i := 0; i < 2*exitDelay; i++ {
		if left, exit := c.tick(true); left != -1 || exit {
			t.Fatalf("after Stay open: %d %v", left, exit)
		}
	}
}
