package logbook

import (
	"strings"
	"time"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/utility"
)

// Minutes is how long a session is in Logbook: whole minutes, at least one,
// since the sheet rejects zero.
func Minutes(seconds int64) int {
	minutes := int((seconds + 30) / 60)
	if minutes < 1 {
		return 1
	}
	return minutes
}

// Changes counts what a merge did to the local log.
type Changes struct {
	Added   int // rows added on the site
	Updated int // linked sessions edited on the site
	Deleted int // linked sessions deleted on the site
	Linked  int // local sessions matched to a row they had created without hearing back
}

// Any reports whether the merge changed anything.
func (c Changes) Any() bool {
	return c != Changes{}
}

// Merge applies the sheet to the local log, and the site wins. A session
// linked to a row takes the row's game, start, length and platform, and is
// dropped if the row is gone. Rows no session is linked to are matched to an
// unlinked session with the same start and game, which happens when an upload
// reached the site but its response never came back, and otherwise added as
// new sessions. Unlinked sessions are kept for uploading. Times are written in
// loc, like the sessions gametrak records.
func Merge(local []models.SessionLog, rows []Row, loc *time.Location) ([]models.SessionLog, Changes) {
	var changes Changes
	byID := make(map[string]Row, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	merged := make([]models.SessionLog, 0, len(local))
	linked := make(map[string]bool)
	var unlinked []int // indexes into merged
	for _, s := range local {
		if s.LogbookID == "" {
			unlinked = append(unlinked, len(merged))
			merged = append(merged, s)
			continue
		}
		row, ok := byID[s.LogbookID]
		if !ok {
			changes.Deleted++
			continue
		}
		linked[row.ID] = true
		if updated, ok := apply(s, row, loc); ok {
			if updated != s {
				changes.Updated++
			}
			s = updated
		}
		merged = append(merged, s)
	}

	for _, row := range rows {
		if linked[row.ID] {
			continue
		}
		start, err := parseTime(row.OccurredAt)
		if err != nil {
			continue
		}
		if i := matchUnlinked(merged, unlinked, row, start); i >= 0 {
			if updated, ok := apply(merged[i], row, loc); ok {
				merged[i] = updated
				changes.Linked++
			}
			continue
		}
		if added, ok := apply(models.SessionLog{}, row, loc); ok {
			merged = append(merged, added)
			changes.Added++
		}
	}

	return merged, changes
}

// matchUnlinked finds the unlinked session that started at start and plays the
// row's game, or returns -1.
func matchUnlinked(merged []models.SessionLog, unlinked []int, row Row, start time.Time) int {
	for _, i := range unlinked {
		s := merged[i]
		if s.LogbookID != "" || !strings.EqualFold(strings.TrimSpace(s.Game), strings.TrimSpace(row.Data.Game)) {
			continue
		}
		if t, err := s.StartTime(); err == nil && t.Equal(start) {
			return i
		}
	}
	return -1
}

// apply copies a row's values onto a session. The start and end are only
// rewritten when the instant or length changed, so a session keeps its exact
// seconds and offset unless the site edited them. It reports false if the
// row's time can't be read.
func apply(s models.SessionLog, row Row, loc *time.Location) (models.SessionLog, bool) {
	start, err := parseTime(row.OccurredAt)
	if err != nil {
		return s, false
	}

	s.LogbookID = row.ID
	s.Game = utility.SanitizeTitle(row.Data.Game)
	s.Platform = row.Data.Platform

	current, err := s.StartTime()
	moved := err != nil || !current.Equal(start)
	resized := s.DurationSeconds <= 0 || Minutes(s.DurationSeconds) != row.Data.Minutes
	if resized {
		s.DurationSeconds = int64(row.Data.Minutes) * 60
	}
	if moved || resized {
		start = start.In(loc)
		s.Start = start.Format(time.RFC3339)
		s.End = start.Add(s.Duration()).Format(time.RFC3339)
	}
	return s, true
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
