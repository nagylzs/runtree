package gtkui

import (
	"strings"
	"testing"

	"github.com/nagylzs/runtree/internal/signal"
)

// The Signal menu shows the name of the signal on its button (e.g. SIGABRT, not SIGABORT), and the button has a color
func TestSignalButtons(t *testing.T) {
	for name, b := range signalButtons {
		if _, ok := signal.Signals[name]; !ok {
			t.Errorf("%s is not a signal", name)
		}
		if !strings.HasSuffix(b.label, " "+name) {
			t.Errorf("the label of %s is %q", name, b.label)
		}
		if strings.ContainsRune(b.label, '️') && !strings.HasPrefix(b.label, "☠️") {
			t.Errorf("the label of %s has a variation selector after text: %q", name, b.label)
		}
		if !strings.Contains(mainCss, "."+b.class+" {") {
			t.Errorf("main.css has no rule for %s (%s)", b.class, name)
		}
	}
}
