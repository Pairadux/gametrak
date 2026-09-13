package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/austincgause/gametrak/internal/output"
	"github.com/austincgause/gametrak/internal/stats"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

// barWidth is the character width of the proportional bars in stats output.
const barWidth = 12

var (
	statsBy   string
	statsTop  int
	statsJSON bool
)

var statsCmd = &cobra.Command{
	Use:   "stats [filters...]",
	Short: "Show aggregate game time statistics",
	Long: `Display statistics about your game time including totals per game,
session counts, averages, and play streaks. When the filter covers a bounded
period, totals are compared against the period before it.

Use --by to add a breakdown: day, week, month, weekday, or hour.

` + filterArgsUsage,
	Example: `  gametrak stats
  gametrak stats week
  gametrak stats month --by day
  gametrak stats --by hour
  gametrak stats 2026-01 --top 5`,
	ValidArgs: []string{"today", "yesterday", "week", "month", "year", "all"},
	RunE: func(cmd *cobra.Command, args []string) error {
		var period stats.Period
		if statsBy != "" {
			parsed, err := stats.ParsePeriod(statsBy)
			if err != nil {
				return err
			}
			period = parsed
		}

		q, err := runQuery(args)
		if err != nil {
			return err
		}
		if q.report() {
			return nil
		}

		now := time.Now()
		summary := stats.Summarize(q.Matched, now)

		var buckets []stats.Bucket
		if statsBy != "" {
			buckets = stats.Bucketize(q.Matched, period, now)
		}

		if statsJSON {
			return printJSON(q, summary, buckets)
		}

		header := "Game Statistics"
		if q.Filter.Active() {
			header += fmt.Sprintf(" (%s)", q.Filter.Describe())
		}
		fmt.Println(header)
		fmt.Printf("%s\n\n", strings.Repeat("=", len(header)))

		printSummary(summary, previousSummary(q, now))
		printByGame(summary)
		printBreakdown(buckets, statsBy)

		fmt.Println()
		return nil
	},
}

// durationTable builds a table whose first column is a label followed by the
// four cells from utility.DurationCells, then a bar and a session count.
func durationTable() *output.Table {
	return output.NewTable(output.Left, output.Right, output.Left, output.Right, output.Left, output.Left, output.Left).
		Gaps("  ", " ", "  ", " ", "  ", "  ")
}

// previousSummary aggregates the period immediately before the filtered one,
// or nil when there is nothing comparable to measure against.
func previousSummary(q sessionQuery, now time.Time) *stats.Summary {
	previous, ok := q.Filter.Previous(now)
	if !ok {
		return nil
	}
	summary := stats.Summarize(previous.Apply(q.All), now)
	if summary.Sessions == 0 {
		return nil
	}
	return &summary
}

func printJSON(q sessionQuery, summary stats.Summary, buckets []stats.Bucket) error {
	payload := struct {
		Filter  string         `json:"filter,omitempty"`
		Summary stats.Summary  `json:"summary"`
		Buckets []stats.Bucket `json:"buckets,omitempty"`
	}{
		Filter:  q.Filter.Describe(),
		Summary: summary,
		Buckets: buckets,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func printSummary(s stats.Summary, previous *stats.Summary) {
	table := output.NewTable(output.Left, output.Left)
	table.Row("Total", fmt.Sprintf("%s across %s",
		utility.FormatDurationRounded(s.Total), utility.Plural(s.Sessions, "session")))
	table.Row("Average", fmt.Sprintf("%s per session", utility.FormatDurationRounded(s.Average())))
	table.Row("Per day", fmt.Sprintf("%s across %s played",
		utility.FormatDurationRounded(s.DailyAverage()), utility.Plural(s.DaysPlayed, "day")))

	if start, err := s.Longest.StartTime(); err == nil {
		table.Row("Longest", fmt.Sprintf("%s, %s on %s",
			s.Longest.Game, utility.FormatDurationRounded(s.Longest.Duration()), start.Format("2006-01-02")))
	}

	streak := "none"
	if s.CurrentStreak > 0 {
		streak = utility.Plural(s.CurrentStreak, "day")
	}
	table.Row("Streak", fmt.Sprintf("%s (longest %s)", streak, utility.Plural(s.LongestStreak, "day")))

	if !s.First.IsZero() {
		table.Row("Span", fmt.Sprintf("%s to %s", s.First.Format("2006-01-02"), s.Last.Format("2006-01-02")))
	}

	if previous != nil {
		table.Row("Change", fmt.Sprintf("%s vs the period before (%s)",
			utility.FormatDelta(s.Total-previous.Total),
			utility.FormatDurationRounded(previous.Total)))
	}

	table.Print(os.Stdout)
	fmt.Println()
}

func printByGame(s stats.Summary) {
	games := s.ByGame
	if statsTop > 0 && statsTop < len(games) {
		games = games[:statsTop]
	}

	fmt.Println("By game:")
	table := durationTable()
	longest := float64(0)
	if len(s.ByGame) > 0 {
		longest = s.ByGame[0].Duration.Seconds()
	}

	for _, g := range games {
		cells := append([]string{g.Game}, utility.DurationCells(g.Duration)...)
		cells = append(cells,
			output.Bar(g.Duration.Seconds(), longest, barWidth),
			fmt.Sprintf("(%s)", utility.Plural(g.Sessions, "session")))
		table.Row(cells...)
	}
	table.Print(os.Stdout)

	if hidden := len(s.ByGame) - len(games); hidden > 0 {
		fmt.Printf("  ... and %s\n", utility.Plural(hidden, "more game"))
	}
}

func printBreakdown(buckets []stats.Bucket, period string) {
	if len(buckets) == 0 {
		return
	}

	peak := float64(0)
	for _, b := range buckets {
		if s := b.Duration.Seconds(); s > peak {
			peak = s
		}
	}

	fmt.Printf("\nBy %s:\n", period)
	table := durationTable()
	for _, b := range buckets {
		cells := append([]string{b.Label}, utility.DurationCells(b.Duration)...)
		cells = append(cells, output.Bar(b.Duration.Seconds(), peak, barWidth))
		if b.Sessions > 0 {
			cells = append(cells, fmt.Sprintf("(%s)", utility.Plural(b.Sessions, "session")))
		}
		table.Row(cells...)
	}
	table.Print(os.Stdout)
}

func init() {
	rootCmd.AddCommand(statsCmd)

	statsCmd.Flags().StringVar(&statsBy, "by", "", "break down totals by day, week, month, weekday, or hour")
	statsCmd.Flags().IntVar(&statsTop, "top", 0, "show only the top N games")
	statsCmd.Flags().BoolVar(&statsJSON, "json", false, "output raw statistics as JSON")
}
