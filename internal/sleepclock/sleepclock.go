// Package sleepclock measures how long the machine has been suspended, so
// time spent asleep with a game open is not counted as played.
//
// Linux has two clocks that both start at boot: CLOCK_BOOTTIME counts time
// asleep and CLOCK_MONOTONIC does not, so their difference is the total time
// suspended since boot. Taking it at a session's start and end gives the sleep
// in between, with no signal to catch and nothing to poll.
package sleepclock

import (
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Clock reports the total time asleep since boot, and which boot it is.
type Clock interface {
	Asleep() time.Duration
	Boot() string
}

// Slept returns how long the machine has slept since asleepAt was read from
// c during boot. counted is the sleep already known; it's the answer when the
// reading came from another boot, whose counter can't be compared with this
// one's, and the least it can be otherwise.
func Slept(c Clock, boot string, asleepAt, counted time.Duration) time.Duration {
	if boot != c.Boot() {
		return counted
	}
	if d := c.Asleep() - asleepAt; d > counted {
		return d
	}
	return counted
}

// System is the machine's real clock.
type System struct{}

// Asleep returns how long the machine has been suspended since it booted.
func (System) Asleep() time.Duration {
	var boot, mono unix.Timespec
	if unix.ClockGettime(unix.CLOCK_BOOTTIME, &boot) != nil || unix.ClockGettime(unix.CLOCK_MONOTONIC, &mono) != nil {
		return 0
	}
	if d := time.Duration(boot.Nano() - mono.Nano()); d > 0 {
		return d
	}
	return 0
}

// Boot returns the kernel's ID for the current boot, or "" if it can't be read.
// Asleep restarts from zero on every boot, so readings only compare within one.
func (System) Boot() string {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
