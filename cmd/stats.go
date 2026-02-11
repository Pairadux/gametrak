package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/austincgause/gametrak/internal/session"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats [today|yesterday|week|month|year|YYYY-MM-DD|<game>]",
	Short: "Show aggregate game time statistics",
	Long: `Display statistics about your game time including:
- Total time played per game
- Most played games
- Session counts

Time filters: today, yesterday, week, month, year, or a specific date (YYYY-MM-DD).`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"today", "yesterday", "week", "month", "year"},
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := session.LoadAll(cfg.Settings.SessionsFile)
		if err != nil {
			return fmt.Errorf("failed to load sessions: %w", err)
		}

		if len(sessions) == 0 {
			fmt.Println("No sessions recorded yet.")
			return nil
		}

		timeFilter, gameFilter, err := parseFilterArg(args)
		if err != nil {
			return err
		}

		// Apply filters
		sessions = filterSessions(sessions, timeFilter, gameFilter)

		if len(sessions) == 0 {
			fmt.Println("No sessions match the filter criteria.")
			return nil
		}

		// Aggregate stats per game
		type gameStat struct {
			name         string
			totalSeconds int64
			sessionCount int
		}

		gameStats := make(map[string]*gameStat)

		for _, s := range sessions {
			stat, exists := gameStats[s.Game]
			if !exists {
				stat = &gameStat{name: s.Game}
				gameStats[s.Game] = stat
			}
			stat.totalSeconds += s.DurationSeconds
			stat.sessionCount++
		}

		// Sort by total time
		var stats []*gameStat
		for _, s := range gameStats {
			stats = append(stats, s)
		}
		sort.Slice(stats, func(i, j int) bool {
			return stats[i].totalSeconds > stats[j].totalSeconds
		})

		// Calculate total
		var totalSeconds int64
		var totalSessions int
		for _, s := range stats {
			totalSeconds += s.totalSeconds
			totalSessions += s.sessionCount
		}

		// Build header
		header := "Game Statistics"
		if timeFilter != "" {
			header += fmt.Sprintf(" (%s)", timeFilter)
		} else if gameFilter != "" {
			header += fmt.Sprintf(" (filtered: %s)", gameFilter)
		}
		fmt.Printf("%s\n", header)
		fmt.Printf("%s\n\n", strings.Repeat("=", len(header)))

		fmt.Printf("Total: %s across %d sessions\n\n",
			utility.FormatDurationRounded(time.Duration(totalSeconds)*time.Second),
			totalSessions)

		fmt.Printf("By Game:\n")
		for _, s := range stats {
			duration := time.Duration(s.totalSeconds) * time.Second
			fmt.Printf("  %-20s  %s  (%d sessions)\n",
				s.name,
				utility.FormatDurationRounded(duration),
				s.sessionCount)
		}

		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statsCmd)
}
