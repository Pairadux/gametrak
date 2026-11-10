package sleepclock

import (
	"testing"
	"time"
)

func TestSystemClockReadsTheKernel(t *testing.T) {
	var clock System
	first := clock.Asleep()
	if first < 0 {
		t.Errorf("Asleep = %v, want at least zero", first)
	}
	// Nothing sleeps during a test, so the reading barely moves.
	if drift := clock.Asleep() - first; drift < 0 || drift > time.Second {
		t.Errorf("Asleep moved by %v between two reads while awake", drift)
	}
	if boot := clock.Boot(); len(boot) != 36 || boot != clock.Boot() {
		t.Errorf("Boot = %q, want the same 36-character boot ID each time", boot)
	}
}
