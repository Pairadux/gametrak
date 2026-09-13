// Package stats aggregates session logs into totals, streaks, and time buckets.
package stats

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/austincgause/gametrak/internal/models"
)

// GameTotal is the aggregate playtime for a single game.
type GameTotal struct {
	Game     string
	Duration time.Duration
	Sessions int
	Last     time.Time
}

// Summary describes a set of sessions as a whole.
type Summary struct {
	Total         time.Duration
	Sessions      int
	ByGame        []GameTotal
	Longest       models.SessionLog
	First         time.Time
	Last          time.Time
	DaysPlayed    int
	CurrentStreak int
	LongestStreak int
}

// Average returns the mean session length, or zero when there are no sessions.
func (s Summary) Average() time.Duration {
	if s.Sessions == 0 {
		return 0
	}
	return s.Total / time.Duration(s.Sessions)
}

// DailyAverage returns the mean playtime across days that had at least one
// session, or zero when nothing was played.
func (s Summary) DailyAverage() time.Duration {
	if s.DaysPlayed == 0 {
		return 0
	}
	return s.Total / time.Duration(s.DaysPlayed)
}

// Summarize aggregates sessions, reporting streaks relative to now. Sessions
// with unparseable timestamps are ignored.
func Summarize(sessions []models.SessionLog, now time.Time) Summary {
	loc := now.Location()
	summary := Summary{}
	totals := make(map[string]*GameTotal)
	days := make(map[time.Time]bool)

	for _, s := range sessions {
		start, err := s.StartTime()
		if err != nil {
			continue
		}
		start = start.In(loc)

		summary.Total += s.Duration()
		summary.Sessions++
		days[startOfDay(start)] = true

		if s.DurationSeconds > summary.Longest.DurationSeconds {
			summary.Longest = s
		}
		if summary.First.IsZero() || start.Before(summary.First) {
			summary.First = start
		}
		if start.After(summary.Last) {
			summary.Last = start
		}

		total, ok := totals[s.Game]
		if !ok {
			total = &GameTotal{Game: s.Game}
			totals[s.Game] = total
		}
		total.Duration += s.Duration()
		total.Sessions++
		if start.After(total.Last) {
			total.Last = start
		}
	}

	for _, total := range totals {
		summary.ByGame = append(summary.ByGame, *total)
	}
	sort.Slice(summary.ByGame, func(i, j int) bool {
		if summary.ByGame[i].Duration != summary.ByGame[j].Duration {
			return summary.ByGame[i].Duration > summary.ByGame[j].Duration
		}
		return summary.ByGame[i].Game < summary.ByGame[j].Game
	})

	summary.DaysPlayed = len(days)
	summary.CurrentStreak, summary.LongestStreak = streaks(days, startOfDay(now))
	return summary
}

// streaks returns the streak still running as of today and the longest streak
// ever recorded. A streak survives until a full day passes with no sessions, so
// playing yesterday but not yet today still counts as current.
func streaks(days map[time.Time]bool, today time.Time) (current, longest int) {
	if len(days) == 0 {
		return 0, 0
	}

	sorted := make([]time.Time, 0, len(days))
	for day := range days {
		sorted = append(sorted, day)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })

	run := 1
	longest = 1
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Equal(sorted[i-1].AddDate(0, 0, 1)) {
			run++
		} else {
			run = 1
		}
		if run > longest {
			longest = run
		}
	}

	last := sorted[len(sorted)-1]
	if last.Equal(today) || last.Equal(today.AddDate(0, 0, -1)) {
		current = run
	}
	return current, longest
}

// Period is the granularity of a time bucket.
type Period int

const (
	Daily Period = iota
	Weekly
	Monthly
)

// ParsePeriod resolves a period name such as "day", "week", or "month".
func ParsePeriod(name string) (Period, error) {
	switch strings.ToLower(name) {
	case "day", "daily":
		return Daily, nil
	case "week", "weekly":
		return Weekly, nil
	case "month", "monthly":
		return Monthly, nil
	default:
		return 0, fmt.Errorf("unknown period %q (want day, week, or month)", name)
	}
}

// Bucket is the aggregate playtime for one time period.
type Bucket struct {
	Label    string
	Start    time.Time
	Duration time.Duration
	Sessions int
}

// Bucketize groups sessions into consecutive periods, sorted oldest first.
// Periods with no sessions are included so gaps are visible.
func Bucketize(sessions []models.SessionLog, period Period, now time.Time) []Bucket {
	loc := now.Location()
	totals := make(map[time.Time]*Bucket)

	for _, s := range sessions {
		start, err := s.StartTime()
		if err != nil {
			continue
		}
		key := truncate(start.In(loc), period)

		bucket, ok := totals[key]
		if !ok {
			bucket = &Bucket{Label: label(key, period), Start: key}
			totals[key] = bucket
		}
		bucket.Duration += s.Duration()
		bucket.Sessions++
	}

	if len(totals) == 0 {
		return nil
	}

	keys := make([]time.Time, 0, len(totals))
	for key := range totals {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })

	var buckets []Bucket
	for key := keys[0]; !key.After(keys[len(keys)-1]); key = next(key, period) {
		if bucket, ok := totals[key]; ok {
			buckets = append(buckets, *bucket)
			continue
		}
		buckets = append(buckets, Bucket{Label: label(key, period), Start: key})
	}
	return buckets
}

func truncate(t time.Time, period Period) time.Time {
	day := startOfDay(t)
	switch period {
	case Weekly:
		return day.AddDate(0, 0, -int(day.Weekday()))
	case Monthly:
		y, m, _ := day.Date()
		return time.Date(y, m, 1, 0, 0, 0, 0, day.Location())
	default:
		return day
	}
}

func next(t time.Time, period Period) time.Time {
	switch period {
	case Weekly:
		return t.AddDate(0, 0, 7)
	case Monthly:
		return t.AddDate(0, 1, 0)
	default:
		return t.AddDate(0, 0, 1)
	}
}

func label(t time.Time, period Period) string {
	switch period {
	case Weekly:
		return "week of " + t.Format("2006-01-02")
	case Monthly:
		return t.Format("2006-01")
	default:
		return t.Format("2006-01-02 Mon")
	}
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
