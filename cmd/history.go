package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/austincgause/gametrak/internal/output"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

var (
	historyLimit int
	historyAll   bool
)

var historyCmd = &cobra.Command{
	Use:   "history [filters...]",
	Short: "Display recent game sessions",
	Long: `Display a log of recent game sessions with rounded times.

By default shows the last 10 sessions. Use --all to show all sessions
or --limit to specify a different number. Any filter shows every match.

` + filterArgsUsage,
	Example: `  gametrak history
  gametrak history today
  gametrak history week rimworld
  gametrak history 2026-01-01..2026-01-31`,
	ValidArgs: []string{"today", "yesterday", "week", "month", "year", "all"},
	RunE: func(cmd *cobra.Command, args []string) error {
		q, err := runQuery(args)
		if err != nil {
			return err
		}
		if q.report() {
			return nil
		}

		sessions := q.Matched
		sortByStart(sessions, true)

		// An explicit filter is a deliberate request, so it overrides the
		// default row limit.
		if !historyAll && !q.Filter.Active() && historyLimit > 0 && historyLimit < len(sessions) {
			sessions = sessions[:historyLimit]
		}

		header := "Recent game sessions"
		if q.Filter.Active() {
			header = fmt.Sprintf("Game sessions (%s)", q.Filter.Describe())
		}
		fmt.Printf("%s:\n\n", header)

		table := output.NewTable(output.Left, output.Left, output.Right, output.Left, output.Right, output.Left).
			Gaps("  ", "  ", " ", "  ", " ")
		var played time.Duration
		for _, s := range sessions {
			start, err := s.StartTime()
			if err != nil {
				continue
			}
			played += s.Duration()
			cells := append([]string{start.Format("2006-01-02 15:04"), s.Game}, utility.DurationCells(s.Duration())...)
			table.Row(cells...)
		}
		table.Print(os.Stdout)

		fmt.Printf("\n  %d sessions, %s total\n\n", len(sessions), utility.FormatDurationRounded(played))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(historyCmd)

	historyCmd.Flags().IntVarP(&historyLimit, "limit", "l", 10, "number of sessions to show")
	historyCmd.Flags().BoolVarP(&historyAll, "all", "a", false, "show all sessions")
}
