package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/output"
	"github.com/austincgause/gametrak/internal/stats"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

// recentSessionCount is how many sessions the game report lists.
const recentSessionCount = 5

var gameCmd = &cobra.Command{
	Use:   "game <name> [filters...]",
	Short: "Show a detailed report for one game",
	Long: `Report everything recorded for a single game: totals, streaks, the
months you played it, when during the day you play, and your latest sessions.

The name is matched case-insensitively against recorded session names, so a
fragment such as "rim" is enough.`,
	Example: `  gametrak game rimworld
  gametrak game deadlock year`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		q, err := runQuery(args)
		if err != nil {
			return err
		}
		if q.Filter.Game == "" {
			return fmt.Errorf("no game name given")
		}
		if q.report() {
			return nil
		}

		now := time.Now()
		summary := stats.Summarize(q.Matched, now)

		title := strings.Join(names(summary), ", ")
		fmt.Println(title)
		fmt.Printf("%s\n\n", strings.Repeat("=", len(title)))

		table := output.NewTable(output.Left, output.Left)
		table.Row("Total", fmt.Sprintf("%s across %s",
			utility.FormatDurationRounded(summary.Total), utility.Plural(summary.Sessions, "session")))
		table.Row("Average", fmt.Sprintf("%s per session", utility.FormatDurationRounded(summary.Average())))
		table.Row("Longest", utility.FormatDurationRounded(summary.Longest.Duration()))
		table.Row("Days", utility.Plural(summary.DaysPlayed, "day"))
		table.Row("Streak", fmt.Sprintf("%s (longest %s)",
			utility.Plural(summary.CurrentStreak, "day"), utility.Plural(summary.LongestStreak, "day")))
		table.Row("First", summary.First.Format("2006-01-02"))
		table.Row("Last", fmt.Sprintf("%s (%s ago)",
			summary.Last.Format("2006-01-02"), utility.FormatDurationRounded(now.Sub(summary.Last))))
		table.Print(os.Stdout)

		printBreakdown(stats.Bucketize(q.Matched, stats.Monthly, now), "month")
		printBreakdown(busiestHours(q.Matched, now), "hour")

		fmt.Printf("\nLatest sessions:\n")
		recent := q.Matched
		sortByStart(recent, true)
		if len(recent) > recentSessionCount {
			recent = recent[:recentSessionCount]
		}

		sessionTable := output.NewTable(output.Left, output.Right, output.Left, output.Right, output.Left).
			Gaps("  ", " ", "  ", " ")
		for _, s := range recent {
			start, err := s.StartTime()
			if err != nil {
				continue
			}
			sessionTable.Row(append([]string{start.Format("2006-01-02 15:04")}, utility.DurationCells(s.Duration())...)...)
		}
		sessionTable.Print(os.Stdout)

		fmt.Println()
		return nil
	},
}

// names lists the distinct recorded names that matched, so a fragment matching
// several titles (a demo and its full release, say) is not silently collapsed.
func names(summary stats.Summary) []string {
	var found []string
	for _, game := range summary.ByGame {
		found = append(found, game.Game)
	}
	return found
}

// busiestHours drops the hours with no recorded play so the report shows only
// the times of day this game is actually played.
func busiestHours(sessions []models.SessionLog, now time.Time) []stats.Bucket {
	var played []stats.Bucket
	for _, bucket := range stats.Bucketize(sessions, stats.Hourly, now) {
		if bucket.Sessions > 0 {
			played = append(played, bucket)
		}
	}
	return played
}

func init() {
	rootCmd.AddCommand(gameCmd)
}
