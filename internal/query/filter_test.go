package query

import (
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/models"
)

// reference is a fixed Sunday-anchored clock: 2026-09-13 is a Sunday.
var reference = time.Date(2026, 9, 16, 14, 30, 0, 0, time.Local)

func logAt(t time.Time, game string, d time.Duration) models.SessionLog {
	return models.SessionLog{
		Game:            game,
		Start:           t.Format(time.RFC3339),
		End:             t.Add(d).Format(time.RFC3339),
		DurationSeconds: int64(d.Seconds()),
	}
}

func day(y int, m time.Month, d, hour int) time.Time {
	return time.Date(y, m, d, hour, 0, 0, 0, time.Local)
}

func TestParseTimeKeywords(t *testing.T) {
	tests := []struct {
		arg   string
		start time.Time
		end   time.Time // zero means unbounded
	}{
		{"today", day(2026, 9, 16, 0), time.Time{}},
		{"yesterday", day(2026, 9, 15, 0), day(2026, 9, 16, 0)},
		{"week", day(2026, 9, 13, 0), time.Time{}},
		{"month", day(2026, 9, 1, 0), time.Time{}},
		{"year", day(2026, 1, 1, 0), time.Time{}},
		{"2026-09-14", day(2026, 9, 14, 0), day(2026, 9, 15, 0)},
		{"2026-08", day(2026, 8, 1, 0), day(2026, 9, 1, 0)},
		{"7d", day(2026, 9, 10, 0), time.Time{}},
		{"2026-08-01..2026-08-31", day(2026, 8, 1, 0), day(2026, 9, 1, 0)},
	}

	for _, tc := range tests {
		t.Run(tc.arg, func(t *testing.T) {
			f, err := Parse([]string{tc.arg}, reference)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", tc.arg, err)
			}
			if f.Start == nil || !f.Start.Equal(tc.start) {
				t.Errorf("start = %v, want %v", f.Start, tc.start)
			}
			if tc.end.IsZero() {
				if f.End != nil {
					t.Errorf("end = %v, want unbounded", f.End)
				}
				return
			}
			if f.End == nil || !f.End.Equal(tc.end) {
				t.Errorf("end = %v, want %v", f.End, tc.end)
			}
		})
	}
}

func TestParseAllIsUnbounded(t *testing.T) {
	f, err := Parse([]string{"all"}, reference)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if f.Start != nil || f.End != nil {
		t.Errorf("want unbounded window, got %v..%v", f.Start, f.End)
	}
	if !f.Active() {
		t.Error("an explicit 'all' should count as an active filter")
	}
}

func TestParseGameNames(t *testing.T) {
	tests := []struct {
		name string
		args []string
		game string
		time string
	}{
		{"single word", []string{"rimworld"}, "rimworld", ""},
		{"multiple words", []string{"slay", "the", "spire"}, "slay the spire", ""},
		{"combined with time", []string{"week", "rimworld"}, "rimworld", "week"},
		{"time given last", []string{"rimworld", "2026-08"}, "rimworld", "2026-08"},
		{"no args", nil, "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Parse(tc.args, reference)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if f.Game != tc.game {
				t.Errorf("game = %q, want %q", f.Game, tc.game)
			}
			if f.TimeLabel != tc.time {
				t.Errorf("time label = %q, want %q", f.TimeLabel, tc.time)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"future date", []string{"2030-01-01"}},
		{"two time filters", []string{"today", "week"}},
		{"inverted range", []string{"2026-08-31..2026-08-01"}},
		{"malformed range", []string{"2026-08-01..notadate"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.args, reference); err == nil {
				t.Errorf("Parse(%v) succeeded, want error", tc.args)
			}
		})
	}
}

func TestApply(t *testing.T) {
	sessions := []models.SessionLog{
		logAt(day(2026, 9, 16, 10), "RimWorld", time.Hour),
		logAt(day(2026, 9, 15, 20), "Deadlock", 2*time.Hour),
		logAt(day(2026, 9, 10, 20), "RimWorld", 30*time.Minute),
		{Game: "Broken", Start: "not a timestamp"},
	}

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no filter keeps everything", nil, 4},
		{"today", []string{"today"}, 1},
		{"yesterday", []string{"yesterday"}, 1},
		{"game across all time", []string{"rimworld"}, 2},
		{"game and time combined", []string{"7d", "rimworld"}, 2},
		{"window excludes older session", []string{"3d", "rimworld"}, 1},
		{"case insensitive game", []string{"DEADLOCK"}, 1},
		{"no matches", []string{"tetris"}, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Parse(tc.args, reference)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if got := len(f.Apply(sessions)); got != tc.want {
				t.Errorf("matched %d sessions, want %d", got, tc.want)
			}
		})
	}
}

func TestApplySkipsUnparseableStartWhenFiltering(t *testing.T) {
	sessions := []models.SessionLog{{Game: "Broken", Start: "nonsense"}}

	f, err := Parse([]string{"today"}, reference)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got := len(f.Apply(sessions)); got != 0 {
		t.Errorf("matched %d sessions, want 0", got)
	}
}

func TestPrevious(t *testing.T) {
	tests := []struct {
		arg   string
		start time.Time
		end   time.Time
	}{
		// An open window compares against the same elapsed span a period back.
		{"today", day(2026, 9, 15, 0), day(2026, 9, 15, 14).Add(30 * time.Minute)},
		{"week", day(2026, 9, 6, 0), day(2026, 9, 9, 14).Add(30 * time.Minute)},
		// A bounded window shifts wholesale.
		{"yesterday", day(2026, 9, 14, 0), day(2026, 9, 15, 0)},
		{"2026-08", day(2026, 7, 1, 0), day(2026, 8, 1, 0)},
	}

	for _, tc := range tests {
		t.Run(tc.arg, func(t *testing.T) {
			f, err := Parse([]string{tc.arg}, reference)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			previous, ok := f.Previous(reference)
			if !ok {
				t.Fatal("Previous reported no comparable window")
			}
			if !previous.Start.Equal(tc.start) {
				t.Errorf("start = %v, want %v", previous.Start, tc.start)
			}
			if !previous.End.Equal(tc.end) {
				t.Errorf("end = %v, want %v", previous.End, tc.end)
			}
		})
	}
}

func TestPreviousUnavailableForUnboundedWindows(t *testing.T) {
	for _, args := range [][]string{nil, {"all"}, {"rimworld"}} {
		f, err := Parse(args, reference)
		if err != nil {
			t.Fatalf("Parse(%v) returned error: %v", args, err)
		}
		if _, ok := f.Previous(reference); ok {
			t.Errorf("Parse(%v): want no comparable window", args)
		}
	}
}

func TestPreviousKeepsGameFilter(t *testing.T) {
	f, err := Parse([]string{"week", "rimworld"}, reference)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	previous, ok := f.Previous(reference)
	if !ok {
		t.Fatal("Previous reported no comparable window")
	}
	if previous.Game != "rimworld" {
		t.Errorf("game = %q, want %q", previous.Game, "rimworld")
	}
}
