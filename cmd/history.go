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
	Use:   "history [today|yesterday|week|month|year|YYYY-MM-DD|<game>]",
	Short: "Display recent game sessions",
	Long: `Display a log of recent game sessions with rounded times.

By default shows the last 10 sessions. Use --all to show all sessions
or --limit to specify a different number.

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

		// Determine how many to show (only apply limit if no filter is set)
		count := len(sessions)
		hasFilter := timeFilter != "" || gameFilter != ""
		if !historyAll && !hasFilter && historyLimit > 0 && historyLimit < count {
			count = historyLimit
		}

		// Show most recent first
		start := max(len(sessions)-count, 0)

		fmt.Printf("Recent game sessions:\n\n")

		// Collect visible rows for alignment
		type histRow struct {
			date  string
			game  string
			hours int
			mins  int
		}
		var rows []histRow
		maxGameLen := 0
		hasHours := false

		for i := len(sessions) - 1; i >= start; i-- {
			s := sessions[i]
			startTime, err := time.Parse(time.RFC3339, s.Start)
			if err != nil {
				continue
			}
			r := utility.RoundDuration(time.Duration(s.DurationSeconds) * time.Second)
			row := histRow{
				date:  startTime.Format("2006-01-02 15:04"),
				game:  s.Game,
				hours: r.Hours,
				mins:  r.Mins,
			}
			if len(s.Game) > maxGameLen {
				maxGameLen = len(s.Game)
			}
			if r.Hours > 0 {
				hasHours = true
			}
			rows = append(rows, row)
		}

		for _, r := range rows {
			hourWord := "hours"
			if r.hours == 1 {
				hourWord = "hour"
			}

			var line string
			if hasHours {
				if r.hours > 0 {
					line = fmt.Sprintf("  %s  %-*s  %2d %-5s  %2d mins", r.date, maxGameLen, r.game, r.hours, hourWord, r.mins)
				} else {
					line = fmt.Sprintf("  %s  %-*s            %2d mins", r.date, maxGameLen, r.game, r.mins)
				}
			} else {
				line = fmt.Sprintf("  %s  %-*s  %2d mins", r.date, maxGameLen, r.game, r.mins)
			}
			fmt.Println(line)
		}

		fmt.Println()
		return nil
	},
}

// parseFilterArg extracts time and game filters from command arguments.
// It recognizes keywords (today, yesterday, week, month, year),
// dates (YYYY-MM-DD), and treats anything else as a game name filter.
func parseFilterArg(args []string) (timeFilter, gameFilter string, err error) {
	if len(args) == 0 {
		return "", "", nil
	}

	arg := strings.ToLower(args[0])
	switch arg {
	case "today", "yesterday", "week", "month", "year":
		return arg, "", nil
	default:
		date, parseErr := time.Parse("2006-01-02", arg)
		if parseErr != nil {
			return "", args[0], nil
		}

		now := time.Now()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		if date.After(today) {
			return "", "", fmt.Errorf("cannot query future date: %s", arg)
		}
		return arg, "", nil
	}
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

		// Time filter (uses session start time only)
		if timeFilter != "" {
			if !matchesTimeFilter(startTime, now, timeFilter) {
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

func matchesTimeFilter(startTime, now time.Time, filter string) bool {
	loc := now.Location()

	switch filter {
	case "today":
		y, m, d := now.Date()
		startOfDay := time.Date(y, m, d, 0, 0, 0, 0, loc)
		return !startTime.Before(startOfDay)
	case "yesterday":
		y, m, d := now.AddDate(0, 0, -1).Date()
		startOfYesterday := time.Date(y, m, d, 0, 0, 0, 0, loc)
		y, m, d = now.Date()
		startOfToday := time.Date(y, m, d, 0, 0, 0, 0, loc)
		return !startTime.Before(startOfYesterday) && startTime.Before(startOfToday)
	case "week":
		startOfWeek := now.AddDate(0, 0, -int(now.Weekday()))
		y, m, d := startOfWeek.Date()
		startOfWeek = time.Date(y, m, d, 0, 0, 0, 0, loc)
		return !startTime.Before(startOfWeek)
	case "month":
		y, m, _ := now.Date()
		startOfMonth := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		return !startTime.Before(startOfMonth)
	case "year":
		startOfYear := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		return !startTime.Before(startOfYear)
	default:
		// Arbitrary date (YYYY-MM-DD)
		date, err := time.Parse("2006-01-02", filter)
		if err != nil {
			return true
		}
		startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
		endOfDay := startOfDay.AddDate(0, 0, 1)
		return !startTime.Before(startOfDay) && startTime.Before(endOfDay)
	}
}

func init() {
	rootCmd.AddCommand(historyCmd)

	historyCmd.Flags().IntVarP(&historyLimit, "limit", "l", 10, "number of sessions to show")
	historyCmd.Flags().BoolVarP(&historyAll, "all", "a", false, "show all sessions")
}
