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
// history, stats, heatmap, and export commands.
const filterArgsUsage = `Filters may be combined, in any order:
  today, yesterday, week, month, year, all
  30d                     the last 30 days
  2026-01-26              a single day
  2026-01                 a whole month
  2026-01-01..2026-02-14  a range, end inclusive
  <game>                  case-insensitive match on the game name`

// sessionQuery is the result of reading the session log and applying a filter.
// The full set is kept so commands can compare against other windows.
type sessionQuery struct {
	All     []models.SessionLog
	Matched []models.SessionLog
	Filter  query.Filter
}

// Empty reports whether no sessions have ever been recorded.
func (q sessionQuery) Empty() bool { return len(q.All) == 0 }

// NoMatch reports whether sessions exist but none satisfy the filter.
func (q sessionQuery) NoMatch() bool { return len(q.All) > 0 && len(q.Matched) == 0 }

// report prints the reason there is nothing to show, and reports whether the
// caller should stop.
func (q sessionQuery) report() bool {
	switch {
	case q.Empty():
		fmt.Println("No sessions recorded yet.")
		return true
	case q.NoMatch():
		fmt.Printf("No sessions match: %s\n", q.Filter.Describe())
		return true
	}
	return false
}

// runQuery reads the session log and applies the filter parsed from args.
func runQuery(args []string) (sessionQuery, error) {
	all, err := session.LoadAll(cfg.Settings.SessionsFile)
	if err != nil {
		return sessionQuery{}, fmt.Errorf("failed to load sessions: %w", err)
	}

	filter, err := query.Parse(args, time.Now())
	if err != nil {
		return sessionQuery{}, err
	}

	return sessionQuery{All: all, Matched: filter.Apply(all), Filter: filter}, nil
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
