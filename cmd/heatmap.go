package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/stats"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

// defaultHeatmapDays is how far back the calendar reaches when no time filter
// is given.
const defaultHeatmapDays = 365

// shades run from the lightest play day to the heaviest; empty days use a dot
// so the grid stays readable.
var (
	shades   = []rune{'░', '▒', '▓', '█'}
	emptyDay = '·'
)

var heatmapCmd = &cobra.Command{
	Use:   "heatmap [filters...]",
	Short: "Show a calendar heatmap of daily playtime",
	Long: `Render playtime as a calendar grid, one column per week and one row
per weekday. Shading is relative to your own play days, so the darkest cells
are your heaviest quarter of days.

Defaults to the last year when no time filter is given.

` + filterArgsUsage,
	Example: `  gametrak heatmap
  gametrak heatmap year
  gametrak heatmap 2026-01..2026-06 deadlock`,
	RunE: func(cmd *cobra.Command, args []string) error {
		q, err := runQuery(args)
		if err != nil {
			return err
		}
		if q.report() {
			return nil
		}

		now := time.Now()
		start, end := heatmapRange(q, now)
		played := dailyTotals(q.Matched, now)

		header := "Playtime Heatmap"
		if q.Filter.Active() {
			header += fmt.Sprintf(" (%s)", q.Filter.Describe())
		}
		fmt.Println(header)
		fmt.Printf("%s\n\n", strings.Repeat("=", len(header)))

		grid := buildGrid(start, end, played)
		fmt.Print(grid)

		summary := stats.Summarize(q.Matched, now)
		fmt.Printf("\n  %s over %s, %s\n",
			utility.FormatDurationRounded(summary.Total),
			utility.Plural(summary.DaysPlayed, "play day"),
			utility.Plural(summary.Sessions, "session"))
		return nil
	},
}

// heatmapRange resolves the calendar bounds, falling back to the last year.
func heatmapRange(q sessionQuery, now time.Time) (start, end time.Time) {
	end = startOfDay(now)
	if q.Filter.End != nil {
		end = startOfDay(q.Filter.End.AddDate(0, 0, -1))
	}

	if q.Filter.Start != nil {
		return startOfDay(*q.Filter.Start), end
	}
	return end.AddDate(0, 0, -(defaultHeatmapDays - 1)), end
}

// dailyTotals indexes playtime by the calendar day each session started on.
func dailyTotals(sessions []models.SessionLog, now time.Time) map[time.Time]time.Duration {
	totals := make(map[time.Time]time.Duration)
	for _, s := range sessions {
		start, err := s.StartTime()
		if err != nil {
			continue
		}
		totals[startOfDay(start.In(now.Location()))] += s.Duration()
	}
	return totals
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// buildGrid renders the weekday rows, the month ruler above them, and the
// legend below.
func buildGrid(start, end time.Time, played map[time.Time]time.Duration) string {
	gridStart := start.AddDate(0, 0, -int(start.Weekday()))
	weeks := int(end.Sub(gridStart).Hours()/24)/7 + 1

	thresholds := quantiles(played)

	var b strings.Builder
	b.WriteString(strings.TrimRight("      "+monthRuler(gridStart, weeks), " ") + "\n")

	for row := range 7 {
		var line strings.Builder
		line.WriteString(fmt.Sprintf("  %s ", time.Weekday(row).String()[:3]))
		for week := range weeks {
			day := gridStart.AddDate(0, 0, week*7+row)
			if day.Before(start) || day.After(end) {
				line.WriteRune(' ')
				continue
			}
			line.WriteRune(shade(played[day], thresholds))
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteByte('\n')
	}

	b.WriteString("\n  " + legend(thresholds) + "\n")
	return b.String()
}

// monthRuler places month abbreviations above the week each month begins in.
func monthRuler(gridStart time.Time, weeks int) string {
	ruler := []rune(strings.Repeat(" ", weeks))
	for week := range weeks {
		day := gridStart.AddDate(0, 0, week*7)
		// A month starts in this column if the 1st falls within its seven days.
		if day.AddDate(0, 0, 6).Month() == day.Month() && day.Day() != 1 {
			continue
		}
		name := day.AddDate(0, 0, 6).Format("Jan")
		if week+len(name) > weeks || (week > 0 && ruler[week-1] != ' ') {
			continue
		}
		copy(ruler[week:], []rune(name))
	}
	return string(ruler)
}

// quantiles splits the nonzero play days into four groups of roughly equal
// size, so shading reflects your own distribution rather than an absolute scale.
func quantiles(played map[time.Time]time.Duration) []time.Duration {
	var durations []time.Duration
	for _, d := range played {
		if d > 0 {
			durations = append(durations, d)
		}
	}
	if len(durations) == 0 {
		return nil
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	pick := func(p float64) time.Duration {
		return durations[int(p*float64(len(durations)-1))]
	}
	return []time.Duration{pick(0.25), pick(0.5), pick(0.75)}
}

func shade(d time.Duration, thresholds []time.Duration) rune {
	if d <= 0 {
		return emptyDay
	}
	for i, t := range thresholds {
		if d <= t {
			return shades[i]
		}
	}
	return shades[len(shades)-1]
}

func legend(thresholds []time.Duration) string {
	parts := []string{fmt.Sprintf("%c none", emptyDay)}
	for i, t := range thresholds {
		parts = append(parts, fmt.Sprintf("%c <=%s", shades[i], utility.FormatDurationRounded(t)))
	}
	if len(thresholds) > 0 {
		parts = append(parts, fmt.Sprintf("%c more", shades[len(shades)-1]))
	}
	return strings.Join(parts, "   ")
}

func init() {
	rootCmd.AddCommand(heatmapCmd)
}
