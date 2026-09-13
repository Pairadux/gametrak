// Package query parses session filter arguments and applies them to session logs.
package query

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/austincgause/gametrak/internal/models"
)

// Filter selects sessions by time window and game name. A nil bound is
// unbounded; Start is inclusive and End is exclusive. Sessions are matched on
// their start time only, so a session belongs to the day it started on.
type Filter struct {
	Start     *time.Time
	End       *time.Time
	Game      string
	TimeLabel string
}

// Parse builds a Filter from positional command arguments. Arguments that look
// like a time window (keywords, dates, months, ranges, or "30d") set the time
// bounds; everything else is joined into a case-insensitive game name match.
func Parse(args []string, now time.Time) (Filter, error) {
	var f Filter
	var gameWords []string

	for _, arg := range args {
		w, ok, err := parseWindow(arg, now)
		if err != nil {
			return Filter{}, err
		}
		if !ok {
			gameWords = append(gameWords, arg)
			continue
		}
		if f.TimeLabel != "" {
			return Filter{}, fmt.Errorf("multiple time filters given: %q and %q", f.TimeLabel, w.label)
		}
		f.Start, f.End, f.TimeLabel = w.start, w.end, w.label
	}

	f.Game = strings.Join(gameWords, " ")
	return f, nil
}

// Active reports whether the filter constrains anything.
func (f Filter) Active() bool {
	return f.TimeLabel != "" || f.Game != ""
}

// Describe returns a human-readable summary of the filter, or "" if inactive.
func (f Filter) Describe() string {
	var parts []string
	if f.TimeLabel != "" {
		parts = append(parts, f.TimeLabel)
	}
	if f.Game != "" {
		parts = append(parts, f.Game)
	}
	return strings.Join(parts, ", ")
}

// Match reports whether a session satisfies the filter. Sessions with an
// unparseable start time never match.
func (f Filter) Match(s models.SessionLog) bool {
	if f.Game != "" && !strings.Contains(strings.ToLower(s.Game), strings.ToLower(f.Game)) {
		return false
	}
	if f.Start == nil && f.End == nil {
		return true
	}

	start, err := s.StartTime()
	if err != nil {
		return false
	}
	if f.Start != nil && start.Before(*f.Start) {
		return false
	}
	if f.End != nil && !start.Before(*f.End) {
		return false
	}
	return true
}

// Apply returns the sessions matching the filter, preserving their order.
func (f Filter) Apply(sessions []models.SessionLog) []models.SessionLog {
	if !f.Active() {
		return sessions
	}
	filtered := make([]models.SessionLog, 0, len(sessions))
	for _, s := range sessions {
		if f.Match(s) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

type window struct {
	start, end *time.Time
	label      string
}

// parseWindow interprets a single argument as a time window. It reports
// ok=false when the argument is not time-like, which the caller treats as part
// of a game name.
func parseWindow(arg string, now time.Time) (window, bool, error) {
	token := strings.ToLower(strings.TrimSpace(arg))
	loc := now.Location()

	switch token {
	case "":
		return window{}, false, nil
	case "all":
		return window{label: "all time"}, true, nil
	case "today":
		return since(startOfDay(now), "today"), true, nil
	case "yesterday":
		start := startOfDay(now).AddDate(0, 0, -1)
		return between(start, startOfDay(now), "yesterday"), true, nil
	case "week":
		start := startOfDay(now).AddDate(0, 0, -int(now.Weekday()))
		return since(start, "week"), true, nil
	case "month":
		y, m, _ := now.Date()
		return since(time.Date(y, m, 1, 0, 0, 0, 0, loc), "month"), true, nil
	case "year":
		return since(time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc), "year"), true, nil
	}

	if from, to, found := strings.Cut(token, ".."); found {
		start, _, err := parsePeriod(from, loc)
		if err != nil {
			return window{}, false, fmt.Errorf("invalid range start %q: %w", from, err)
		}
		_, end, err := parsePeriod(to, loc)
		if err != nil {
			return window{}, false, fmt.Errorf("invalid range end %q: %w", to, err)
		}
		if !start.Before(end) {
			return window{}, false, fmt.Errorf("range start %q is not before range end %q", from, to)
		}
		return between(start, end, from+".."+to), true, nil
	}

	if days, ok := parseRelativeDays(token); ok {
		start := startOfDay(now).AddDate(0, 0, -(days - 1))
		return since(start, fmt.Sprintf("last %d days", days)), true, nil
	}

	start, end, err := parsePeriod(token, loc)
	if err != nil {
		return window{}, false, nil // not time-like; treat as a game name
	}
	if start.After(now) {
		return window{}, false, fmt.Errorf("cannot query future date: %s", arg)
	}
	return between(start, end, token), true, nil
}

// parsePeriod parses a YYYY-MM-DD day or a YYYY-MM month and returns its
// half-open bounds.
func parsePeriod(token string, loc *time.Location) (start, end time.Time, err error) {
	if d, err := time.ParseInLocation("2006-01-02", token, loc); err == nil {
		return d, d.AddDate(0, 0, 1), nil
	}
	if m, err := time.ParseInLocation("2006-01", token, loc); err == nil {
		return m, m.AddDate(0, 1, 0), nil
	}
	return time.Time{}, time.Time{}, fmt.Errorf("expected YYYY-MM-DD or YYYY-MM")
}

// parseRelativeDays parses shorthand like "30d" into a day count.
func parseRelativeDays(token string) (int, bool) {
	digits, ok := strings.CutSuffix(token, "d")
	if !ok || digits == "" {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func since(start time.Time, label string) window {
	return window{start: &start, label: label}
}

func between(start, end time.Time, label string) window {
	return window{start: &start, end: &end, label: label}
}
