package sleepclock

import (
	"testing"
	"time"
)

type fixedClock struct {
	asleep time.Duration
	boot   string
}

func (c fixedClock) Asleep() time.Duration { return c.asleep }
func (c fixedClock) Boot() string          { return c.boot }

func TestSlept(t *testing.T) {
	const start = 10 * time.Minute
	for name, tc := range map[string]struct {
		now     time.Duration
		boot    string
		counted time.Duration
		want    time.Duration
	}{
		"awake":                                {start, "b", 0, 0},
		"the clocks' jitter isn't sleep":       {start + 40*time.Microsecond, "b", 0, 0},
		"nor is anything under a second":       {start + 999*time.Millisecond, "b", 0, 0},
		"a second is":                          {start + time.Second, "b", 0, time.Second},
		"a suspend":                            {start + 2*time.Hour, "b", 0, 2 * time.Hour},
		"never less than already counted":      {start + 5*time.Second, "b", time.Minute, time.Minute},
		"jitter on top of what's counted":      {start + 40*time.Microsecond, "b", time.Minute, time.Minute},
		"another boot keeps what's counted":    {3 * time.Hour, "other", time.Minute, time.Minute},
		"a reading that went back counts none": {start - time.Second, "b", 0, 0},
	} {
		if got := Slept(fixedClock{tc.now, "b"}, tc.boot, start, tc.counted); got != tc.want {
			t.Errorf("%s: Slept = %v, want %v", name, got, tc.want)
		}
	}
}

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
