package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/austincgause/gametrak/internal/session"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show aggregate game time statistics",
	Long: `Display statistics about your game time including:
- Total time played per game
- Most played games
- Session counts

More detailed stats coming in future versions.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := session.LoadAll(cfg.Settings.SessionsFile)
		if err != nil {
			return fmt.Errorf("failed to load sessions: %w", err)
		}

		if len(sessions) == 0 {
			fmt.Println("No sessions recorded yet.")
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

		fmt.Printf("Game Statistics\n")
		fmt.Printf("===============\n\n")

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
