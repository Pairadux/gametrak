package utility

import (
	"fmt"
	"time"
)

// FormatDurationExact formats a duration as "Xh Xm Xs" with exact values
func FormatDurationExact(d time.Duration) string {
	d = d.Round(time.Second)

	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// FormatDurationRounded formats a duration rounded to 15-minute intervals.
// If duration is less than 15 minutes, shows exact minutes.
// Examples: "8 mins", "15 mins", "1 hour", "1 hour 30 mins", "2 hours 15 mins"
func FormatDurationRounded(d time.Duration) string {
	totalMins := int(d.Minutes())

	// Under 15 minutes: show exact
	if totalMins < 15 {
		if totalMins == 1 {
			return "1 min"
		}
		return fmt.Sprintf("%d mins", totalMins)
	}

	// Round to nearest 15 minutes
	rounded := ((totalMins + 7) / 15) * 15

	hours := rounded / 60
	mins := rounded % 60

	if hours == 0 {
		return fmt.Sprintf("%d mins", mins)
	}

	hourWord := "hour"
	if hours > 1 {
		hourWord = "hours"
	}

	if mins == 0 {
		return fmt.Sprintf("%d %s", hours, hourWord)
	}

	return fmt.Sprintf("%d %s %d mins", hours, hourWord, mins)
}

// RoundedDuration holds hours and minutes after rounding to 15-minute intervals.
type RoundedDuration struct {
	Hours, Mins int
}

// RoundDuration rounds a duration to 15-minute intervals and returns
// the separate hour and minute components.
func RoundDuration(d time.Duration) RoundedDuration {
	totalMins := int(d.Minutes())
	if totalMins < 15 {
		return RoundedDuration{Hours: 0, Mins: totalMins}
	}
	rounded := ((totalMins + 7) / 15) * 15
	return RoundedDuration{Hours: rounded / 60, Mins: rounded % 60}
}

// Timestamp returns the current time formatted for logging
func Timestamp() string {
	return time.Now().Format("15:04:05")
}
