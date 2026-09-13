package cmd

import (
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
	statsBy  string
	statsTop int
)

var statsCmd = &cobra.Command{
	Use:   "stats [filters...]",
	Short: "Show aggregate game time statistics",
	Long: `Display statistics about your game time including totals per game,
session counts, averages, and play streaks.

Use --by to add a breakdown over time.

` + filterArgsUsage,
	Example: `  gametrak stats
  gametrak stats week
  gametrak stats month --by day
  gametrak stats 2026 --top 5`,
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

		sessions, filter, total, err := loadFiltered(args)
		if err != nil {
			return err
		}
		if total == 0 {
			fmt.Println("No sessions recorded yet.")
			return nil
		}
		if len(sessions) == 0 {
			fmt.Printf("No sessions match: %s\n", filter.Describe())
			return nil
		}

		now := time.Now()
		summary := stats.Summarize(sessions, now)

		header := "Game Statistics"
		if filter.Active() {
			header += fmt.Sprintf(" (%s)", filter.Describe())
		}
		fmt.Println(header)
		fmt.Printf("%s\n\n", strings.Repeat("=", len(header)))

		printSummary(summary)
		printByGame(summary)

		if statsBy != "" {
			printBreakdown(stats.Bucketize(sessions, period, now), statsBy)
		}

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

func printSummary(s stats.Summary) {
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

	statsCmd.Flags().StringVar(&statsBy, "by", "", "break down totals by day, week, or month")
	statsCmd.Flags().IntVar(&statsTop, "top", 0, "show only the top N games")
}
