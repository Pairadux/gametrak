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

		// Break durations into separate columns for alignment
		type row struct {
			hours, mins int
			sessions    int
		}
		rows := make([]row, len(stats))
		maxNameLen := 0
		hasHours := false
		for i, s := range stats {
			if len(s.name) > maxNameLen {
				maxNameLen = len(s.name)
			}
			r := utility.RoundDuration(time.Duration(s.totalSeconds) * time.Second)
			rows[i] = row{hours: r.Hours, mins: r.Mins, sessions: s.sessionCount}
			if r.Hours > 0 {
				hasHours = true
			}
		}

		fmt.Printf("By Game:\n")
		for i, s := range stats {
			r := rows[i]
			hourWord := "hours"
			if r.hours == 1 {
				hourWord = "hour"
			}

			var line string
			if hasHours {
				if r.hours > 0 {
					line = fmt.Sprintf("  %-*s  %2d %-5s  %2d mins", maxNameLen, s.name, r.hours, hourWord, r.mins)
				} else {
					line = fmt.Sprintf("  %-*s            %2d mins", maxNameLen, s.name, r.mins)
				}
			} else {
				line = fmt.Sprintf("  %-*s  %2d mins", maxNameLen, s.name, r.mins)
			}

			fmt.Printf("%s  (%d sessions)\n", line, r.sessions)
		}

		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statsCmd)
}
