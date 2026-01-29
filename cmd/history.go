package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/session"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

var (
	historyLimit int
	historyAll   bool
)

var historyCmd = &cobra.Command{
	Use:   "history [today|week|month|<game>]",
	Short: "Display recent game sessions",
	Long: `Display a log of recent game sessions with rounded times.

By default shows the last 10 sessions. Use --all to show all sessions
or --limit to specify a different number.`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"today", "week", "month"},
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := session.LoadAll(cfg.Settings.SessionsFile)
		if err != nil {
			return fmt.Errorf("failed to load sessions: %w", err)
		}

		if len(sessions) == 0 {
			fmt.Println("No sessions recorded yet.")
			return nil
		}

		// Parse filter from argument
		var timeFilter, gameFilter string
		if len(args) > 0 {
			arg := strings.ToLower(args[0])
			switch arg {
			case "today", "week", "month":
				timeFilter = arg
			default:
				gameFilter = args[0]
			}
		}

		// Apply filters
		sessions = filterSessions(sessions, timeFilter, gameFilter)

		if len(sessions) == 0 {
			fmt.Println("No sessions match the filter criteria.")
			return nil
		}

		// Determine how many to show (only apply limit if no filter is set)
		count := len(sessions)
		hasFilter := timeFilter != "" || gameFilter != ""
		if !historyAll && !hasFilter && historyLimit > 0 && historyLimit < count {
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

func filterSessions(sessions []models.SessionLog, timeFilter, gameFilter string) []models.SessionLog {
	if timeFilter == "" && gameFilter == "" {
		return sessions
	}

	var filtered []models.SessionLog
	now := time.Now()

	for _, s := range sessions {
		startTime, err := time.Parse(time.RFC3339, s.Start)
		if err != nil {
			continue
		}

		// Time filter
		switch timeFilter {
		case "today":
			y, m, d := now.Date()
			startOfDay := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
			if startTime.Before(startOfDay) {
				continue
			}
		case "week":
			startOfWeek := now.AddDate(0, 0, -int(now.Weekday()))
			y, m, d := startOfWeek.Date()
			startOfWeek = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
			if startTime.Before(startOfWeek) {
				continue
			}
		case "month":
			y, m, _ := now.Date()
			startOfMonth := time.Date(y, m, 1, 0, 0, 0, 0, now.Location())
			if startTime.Before(startOfMonth) {
				continue
			}
		}

		// Game filter (case-insensitive substring match)
		if gameFilter != "" {
			if !strings.Contains(strings.ToLower(s.Game), strings.ToLower(gameFilter)) {
				continue
			}
		}

		filtered = append(filtered, s)
	}

	return filtered
}

func init() {
	rootCmd.AddCommand(historyCmd)

	historyCmd.Flags().IntVarP(&historyLimit, "limit", "l", 10, "number of sessions to show")
	historyCmd.Flags().BoolVarP(&historyAll, "all", "a", false, "show all sessions")
}
