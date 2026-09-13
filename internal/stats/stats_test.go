package stats

import (
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/models"
)

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

func TestSummarizeTotals(t *testing.T) {
	sessions := []models.SessionLog{
		logAt(day(2026, 9, 16, 10), "RimWorld", time.Hour),
		logAt(day(2026, 9, 16, 20), "Deadlock", 3*time.Hour),
		logAt(day(2026, 9, 15, 20), "Deadlock", 2*time.Hour),
		{Game: "Broken", Start: "nonsense"},
	}

	s := Summarize(sessions, reference)

	if want := 6 * time.Hour; s.Total != want {
		t.Errorf("total = %v, want %v", s.Total, want)
	}
	if s.Sessions != 3 {
		t.Errorf("sessions = %d, want 3", s.Sessions)
	}
	if s.DaysPlayed != 2 {
		t.Errorf("days played = %d, want 2", s.DaysPlayed)
	}
	if want := 2 * time.Hour; s.Average() != want {
		t.Errorf("average = %v, want %v", s.Average(), want)
	}
	if want := 3 * time.Hour; s.DailyAverage() != want {
		t.Errorf("daily average = %v, want %v", s.DailyAverage(), want)
	}
	if s.Longest.Game != "Deadlock" || s.Longest.DurationSeconds != int64((3*time.Hour).Seconds()) {
		t.Errorf("longest = %+v, want the 3 hour Deadlock session", s.Longest)
	}
}

func TestSummarizeRanksGamesByTime(t *testing.T) {
	sessions := []models.SessionLog{
		logAt(day(2026, 9, 16, 10), "RimWorld", time.Hour),
		logAt(day(2026, 9, 16, 20), "Deadlock", 3*time.Hour),
		logAt(day(2026, 9, 15, 20), "Deadlock", 2*time.Hour),
	}

	s := Summarize(sessions, reference)

	if len(s.ByGame) != 2 {
		t.Fatalf("games = %d, want 2", len(s.ByGame))
	}
	if s.ByGame[0].Game != "Deadlock" {
		t.Errorf("first game = %q, want Deadlock", s.ByGame[0].Game)
	}
	if s.ByGame[0].Duration != 5*time.Hour {
		t.Errorf("Deadlock total = %v, want 5h", s.ByGame[0].Duration)
	}
	if s.ByGame[0].Sessions != 2 {
		t.Errorf("Deadlock sessions = %d, want 2", s.ByGame[0].Sessions)
	}
	if s.ByGame[0].Seconds != 18000 {
		t.Errorf("Deadlock seconds = %d, want 18000", s.ByGame[0].Seconds)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	s := Summarize(nil, reference)

	if s.Sessions != 0 || s.Total != 0 {
		t.Errorf("empty summary = %+v, want zeroes", s)
	}
	if s.Average() != 0 || s.DailyAverage() != 0 {
		t.Error("averages should be zero when nothing was played")
	}
	if s.CurrentStreak != 0 || s.LongestStreak != 0 {
		t.Error("streaks should be zero when nothing was played")
	}
}

func TestStreaks(t *testing.T) {
	tests := []struct {
		name    string
		days    []time.Time
		current int
		longest int
	}{
		{
			name:    "running streak ending today",
			days:    []time.Time{day(2026, 9, 14, 0), day(2026, 9, 15, 0), day(2026, 9, 16, 0)},
			current: 3,
			longest: 3,
		},
		{
			// A streak is not broken until a whole day passes without play.
			name:    "streak ending yesterday still counts",
			days:    []time.Time{day(2026, 9, 14, 0), day(2026, 9, 15, 0)},
			current: 2,
			longest: 2,
		},
		{
			name:    "stale streak is not current",
			days:    []time.Time{day(2026, 9, 1, 0), day(2026, 9, 2, 0), day(2026, 9, 3, 0)},
			current: 0,
			longest: 3,
		},
		{
			name:    "longest is kept from an earlier run",
			days:    []time.Time{day(2026, 9, 1, 0), day(2026, 9, 2, 0), day(2026, 9, 3, 0), day(2026, 9, 16, 0)},
			current: 1,
			longest: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sessions []models.SessionLog
			for _, d := range tc.days {
				sessions = append(sessions, logAt(d.Add(20*time.Hour), "RimWorld", time.Hour))
			}

			s := Summarize(sessions, reference)
			if s.CurrentStreak != tc.current {
				t.Errorf("current streak = %d, want %d", s.CurrentStreak, tc.current)
			}
			if s.LongestStreak != tc.longest {
				t.Errorf("longest streak = %d, want %d", s.LongestStreak, tc.longest)
			}
		})
	}
}

func TestBucketizeDailyIncludesGaps(t *testing.T) {
	sessions := []models.SessionLog{
		logAt(day(2026, 9, 14, 10), "RimWorld", time.Hour),
		logAt(day(2026, 9, 16, 10), "RimWorld", 2*time.Hour),
	}

	buckets := Bucketize(sessions, Daily, reference)

	if len(buckets) != 3 {
		t.Fatalf("buckets = %d, want 3 (the empty day between is kept)", len(buckets))
	}
	if buckets[1].Sessions != 0 || buckets[1].Duration != 0 {
		t.Errorf("middle bucket = %+v, want an empty day", buckets[1])
	}
	if buckets[2].Duration != 2*time.Hour {
		t.Errorf("last bucket = %v, want 2h", buckets[2].Duration)
	}
}

func TestBucketizeMonthly(t *testing.T) {
	sessions := []models.SessionLog{
		logAt(day(2026, 7, 14, 10), "RimWorld", time.Hour),
		logAt(day(2026, 9, 16, 10), "RimWorld", time.Hour),
	}

	buckets := Bucketize(sessions, Monthly, reference)

	if len(buckets) != 3 {
		t.Fatalf("buckets = %d, want 3", len(buckets))
	}
	if buckets[0].Label != "2026-07" || buckets[2].Label != "2026-09" {
		t.Errorf("labels = %q..%q, want 2026-07..2026-09", buckets[0].Label, buckets[2].Label)
	}
}

func TestBucketizeWeekdayCoversEverySlot(t *testing.T) {
	// 2026-09-16 is a Wednesday.
	sessions := []models.SessionLog{logAt(day(2026, 9, 16, 10), "RimWorld", time.Hour)}

	buckets := Bucketize(sessions, Weekday, reference)

	if len(buckets) != 7 {
		t.Fatalf("buckets = %d, want 7", len(buckets))
	}
	if buckets[0].Label != "Sunday" {
		t.Errorf("first label = %q, want Sunday", buckets[0].Label)
	}
	if buckets[3].Sessions != 1 || buckets[3].Duration != time.Hour {
		t.Errorf("Wednesday = %+v, want the single session", buckets[3])
	}
}

func TestBucketizeHourlyUsesStartHour(t *testing.T) {
	// A session spanning midnight counts entirely toward the hour it began in.
	sessions := []models.SessionLog{logAt(day(2026, 9, 16, 23), "RimWorld", 2*time.Hour)}

	buckets := Bucketize(sessions, Hourly, reference)

	if len(buckets) != 24 {
		t.Fatalf("buckets = %d, want 24", len(buckets))
	}
	if buckets[23].Duration != 2*time.Hour {
		t.Errorf("23:00 = %v, want 2h", buckets[23].Duration)
	}
	if buckets[0].Duration != 0 {
		t.Errorf("00:00 = %v, want 0", buckets[0].Duration)
	}
}

func TestParsePeriod(t *testing.T) {
	tests := map[string]Period{
		"day": Daily, "daily": Daily,
		"week": Weekly, "month": Monthly,
		"weekday": Weekday, "dow": Weekday,
		"hour": Hourly, "HOUR": Hourly,
	}

	for name, expected := range tests {
		got, err := ParsePeriod(name)
		if err != nil {
			t.Errorf("ParsePeriod(%q) returned error: %v", name, err)
			continue
		}
		if got != expected {
			t.Errorf("ParsePeriod(%q) = %v, want %v", name, got, expected)
		}
	}

	if _, err := ParsePeriod("fortnight"); err == nil {
		t.Error("ParsePeriod(\"fortnight\") succeeded, want error")
	}
}
