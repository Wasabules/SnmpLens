package tray

import "testing"

// Every method must tolerate a nil controller: Start returns nil when no tray
// appeared, and callers are expected to keep using the same variable rather
// than guarding each call.
func TestNilControllerIsSafe(t *testing.T) {
	var c *Controller
	c.SetStatus("anything")
	c.SetLabels(Labels{Show: "x"})
	c.Stop()
	c.Stop()
}

func TestStatusLine(t *testing.T) {
	if got := StatusLine(3, true); got != "Monitors: 3 · Traps: on" {
		t.Errorf("got %q", got)
	}
	if got := StatusLine(0, false); got != "Monitors: 0 · Traps: off" {
		t.Errorf("got %q", got)
	}
}

func TestDefaultLabelsAreFilled(t *testing.T) {
	l := DefaultLabels()
	if l.Show == "" || l.Quit == "" || l.Status == "" {
		t.Errorf("incomplete defaults: %+v", l)
	}
}
