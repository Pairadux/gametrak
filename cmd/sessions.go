package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/query"
	"github.com/austincgause/gametrak/internal/session"
)

// filterArgsUsage documents the positional filter arguments shared by the
// history, stats, and export commands.
const filterArgsUsage = `Filters may be combined, in any order:
  today, yesterday, week, month, year, all
  30d                     the last 30 days
  2026-01-26              a single day
  2026-01                 a whole month
  2026-01-01..2026-02-14  a range, end inclusive
  <game>                  case-insensitive match on the game name`

// loadFiltered reads the session log and applies the filter parsed from args.
// It returns the matching sessions, the filter that produced them, and the
// total number of sessions on record.
func loadFiltered(args []string) ([]models.SessionLog, query.Filter, int, error) {
	all, err := loadSessions()
	if err != nil {
		return nil, query.Filter{}, 0, err
	}
	filter, err := query.Parse(args, time.Now())
	if err != nil {
		return nil, query.Filter{}, 0, err
	}
	return filter.Apply(all), filter, len(all), nil
}

func loadSessions() ([]models.SessionLog, error) {
	sessions, err := session.LoadAll(cfg.Settings.SessionsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load sessions: %w", err)
	}
	return sessions, nil
}

// sortByStart orders sessions chronologically, newest first when desc.
func sortByStart(sessions []models.SessionLog, desc bool) {
	sort.SliceStable(sessions, func(i, j int) bool {
		a, errA := sessions[i].StartTime()
		b, errB := sessions[j].StartTime()
		if errA != nil || errB != nil {
			return false
		}
		if desc {
			return a.After(b)
		}
		return a.Before(b)
	})
}
