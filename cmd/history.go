package cmd

import (
	"fmt"
	"time"

	"github.com/austincgause/gametrak/internal/session"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

var (
	historyLimit int
	historyAll   bool
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Display recent game sessions",
	Long: `Display a log of recent game sessions with rounded times.

By default shows the last 10 sessions. Use --all to show all sessions
or --limit to specify a different number.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := session.LoadAll(cfg.Settings.SessionsFile)
		if err != nil {
			return fmt.Errorf("failed to load sessions: %w", err)
		}

		if len(sessions) == 0 {
			fmt.Println("No sessions recorded yet.")
			return nil
		}

		// Determine how many to show
		count := len(sessions)
		if !historyAll && historyLimit > 0 && historyLimit < count {
			count = historyLimit
		}

		// Show most recent first
		start := max(len(sessions)-count, 0)

		fmt.Printf("Recent game sessions:\n\n")

		for i := len(sessions) - 1; i >= start; i-- {
			s := sessions[i]

			startTime, err := time.Parse(time.RFC3339, s.Start)
			if err != nil {
				continue
			}

			duration := time.Duration(s.DurationSeconds) * time.Second
			roundedDuration := utility.FormatDurationRounded(duration)

			fmt.Printf("  %s  %-20s  %s\n",
				startTime.Format("2006-01-02 15:04"),
				s.Game,
				roundedDuration)
		}

		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(historyCmd)

	historyCmd.Flags().IntVarP(&historyLimit, "limit", "l", 10, "number of sessions to show")
	historyCmd.Flags().BoolVarP(&historyAll, "all", "a", false, "show all sessions")
}
