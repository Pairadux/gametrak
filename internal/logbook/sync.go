package logbook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/austincgause/gametrak/internal/lockfile"
	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/session"
)

// Platform is what every session gametrak records was played on.
const Platform = "PC"

// minLinkedToGuard is how many linked sessions the log needs before a pull
// that would delete all of them is refused.
const minLinkedToGuard = 3

// Syncer keeps the session log and a Logbook sheet in step. Each sync pulls
// the sheet when its change counter has moved (the site wins), then uploads
// the sessions that have no row yet. Syncs exclude each other across
// processes, so the daemon and `gametrak sync` never upload a session twice.
type Syncer struct {
	Client       *Client
	SessionsFile string
	// StateFile records the sheet version last pulled and which site and
	// sheet the log's row ids belong to; LockFile serializes syncs.
	StateFile string
	LockFile  string
	// Location is the time zone sessions pulled from the site are written in.
	Location *time.Location
	// Logf, when set, reports progress during long uploads.
	Logf func(format string, args ...any)
}

// Result describes what one sync did.
type Result struct {
	Pulled   bool    // the sheet was fetched
	Changes  Changes // what the pull changed locally
	Uploaded int
	Rejected int // sessions the site refused this time
}

// syncState is the StateFile's contents.
type syncState struct {
	Target   string    `json:"target"`
	Version  int64     `json:"version"`
	SyncedAt time.Time `json:"synced_at"`
	// Rejected maps sessions the site refused to its reason. Automatic syncs
	// skip them rather than resend them on every poll; a full sync retries.
	Rejected map[string]string `json:"rejected,omitempty"`
}

// Sync runs one sync. With full, the sheet is pulled even if its version
// hasn't changed, and sessions the site rejected before are tried again.
func (s *Syncer) Sync(ctx context.Context, full bool) (Result, error) {
	var result Result

	release, err := lockfile.Acquire(s.LockFile)
	if err != nil {
		return result, err
	}
	defer release()

	state, err := s.loadState()
	if err != nil {
		return result, err
	}
	local, err := session.LoadAll(s.SessionsFile)
	if err != nil {
		return result, err
	}
	if state.Target != "" && state.Target != s.Client.Target() && linkedCount(local) > 0 {
		return result, fmt.Errorf("sessions in %s are linked to rows on %s, not %s; "+
			"change the logbook settings back, or move the log aside to start over",
			s.SessionsFile, state.Target, s.Client.Target())
	}

	version, err := s.Client.Version(ctx)
	if err != nil {
		return result, err
	}
	if full || version != state.Version || state.Target != s.Client.Target() {
		if result.Changes, err = s.pull(ctx); err != nil {
			return result, err
		}
		result.Pulled = true
		state.Version = version
	}
	state.Target = s.Client.Target()

	if full || state.Rejected == nil {
		state.Rejected = make(map[string]string)
	}
	uploaded, pushErr := s.push(ctx, state.Rejected, &result.Rejected)
	result.Uploaded = uploaded

	// The version moves with every write, including these uploads. If it
	// moved by exactly that many, nothing else changed and the next sync can
	// skip the pull; otherwise leave the old version so it pulls.
	if pushErr == nil && uploaded > 0 {
		if after, err := s.Client.Version(ctx); err == nil && after == version+int64(uploaded) {
			state.Version = after
		}
	}
	state.SyncedAt = time.Now()
	if err := s.saveState(state); err != nil {
		return result, err
	}
	return result, pushErr
}

// pull fetches the whole sheet and merges it into the log.
func (s *Syncer) pull(ctx context.Context) (Changes, error) {
	rows, err := s.Client.Rows(ctx)
	if err != nil {
		return Changes{}, err
	}

	var changes Changes
	var refused error
	err = session.Update(s.SessionsFile, func(local []models.SessionLog) ([]models.SessionLog, bool) {
		merged, merge := Merge(local, rows, s.Location)
		// A sheet holding none of the rows the log is linked to is far more
		// likely the wrong site or a reset database than every row deleted by
		// hand, so don't mirror it. A log with only a couple of linked
		// sessions is exempt, so deleting those on the site still works.
		if linked := linkedCount(local); linked >= minLinkedToGuard && merge.Deleted == linked {
			refused = fmt.Errorf("%s has none of the %d rows that sessions in %s are linked to; not deleting them",
				s.Client.Target(), linked, s.SessionsFile)
			return local, false
		}
		changes = merge
		return merged, changes.Any()
	})
	if err != nil {
		return changes, err
	}
	return changes, refused
}

// push uploads every session without a row, oldest first, except those in
// skip, then records the new row ids in the log. Sessions the site refuses are
// added to skip and counted in rejected. It stops at the first other failure,
// and still records what was uploaded before it.
func (s *Syncer) push(ctx context.Context, skip map[string]string, rejected *int) (uploaded int, err error) {
	local, err := session.LoadAll(s.SessionsFile)
	if err != nil {
		return 0, err
	}
	var pending []models.SessionLog
	for _, sess := range local {
		if _, refused := skip[keyOf(sess).String()]; sess.LogbookID == "" && !refused {
			pending = append(pending, sess)
		}
	}
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].Start < pending[j].Start })

	if len(pending) > 1 && s.Logf != nil {
		s.Logf("Uploading %d sessions to Logbook", len(pending))
	}
	created := make(map[sessionKey]Row)
	for i, sess := range pending {
		row, createErr := s.Client.Create(ctx, newRow(sess))
		var apiErr *APIError
		if errors.As(createErr, &apiErr) && apiErr.Status == http.StatusBadRequest {
			*rejected++
			skip[keyOf(sess).String()] = apiErr.Message
			if s.Logf != nil {
				s.Logf("Warning: Logbook rejected %s from %s: %s", sess.Game, sess.Start, apiErr.Message)
			}
			continue
		}
		if createErr != nil {
			err = createErr
			break
		}
		created[keyOf(sess)] = row
		if done := i + 1; s.Logf != nil && len(pending) > 50 && done%50 == 0 {
			s.Logf("Uploaded %d of %d", done, len(pending))
		}
	}

	if len(created) > 0 {
		updateErr := session.Update(s.SessionsFile, func(local []models.SessionLog) ([]models.SessionLog, bool) {
			for i, sess := range local {
				if row, ok := created[keyOf(sess)]; ok && sess.LogbookID == "" {
					local[i].LogbookID = row.ID
					local[i].Platform = row.Data.Platform
				}
			}
			return local, true
		})
		if updateErr != nil {
			// The rows exist, so the next pull links them by start and game.
			return len(created), updateErr
		}
	}
	return len(created), err
}

func newRow(sess models.SessionLog) NewRow {
	return NewRow{
		OccurredAt: sess.Start,
		Game:       sess.Game,
		Minutes:    Minutes(sess.DurationSeconds),
		Platform:   Platform,
	}
}

// sessionKey identifies an unlinked session between reading the log and
// rewriting it.
type sessionKey struct{ start, class, game string }

func keyOf(s models.SessionLog) sessionKey {
	return sessionKey{s.Start, s.Class, s.Game}
}

func (k sessionKey) String() string {
	return k.start + " " + k.class + " " + k.game
}

func linkedCount(sessions []models.SessionLog) int {
	count := 0
	for _, s := range sessions {
		if s.LogbookID != "" {
			count++
		}
	}
	return count
}

func (s *Syncer) loadState() (syncState, error) {
	var state syncState
	data, err := os.ReadFile(s.StateFile)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("failed to read sync state: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("failed to parse sync state %s: %w", s.StateFile, err)
	}
	return state, nil
}

func (s *Syncer) saveState(state syncState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.StateFile), 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}
	if err := os.WriteFile(s.StateFile, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("failed to save sync state: %w", err)
	}
	return nil
}

// Run syncs whenever trigger fires and every interval (never, if zero) until
// ctx is done, then returns. done receives each sync's outcome.
func (s *Syncer) Run(ctx context.Context, trigger <-chan struct{}, interval time.Duration, done func(Result, error)) {
	var tick <-chan time.Time
	if interval > 0 {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		tick = ticker.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-trigger:
		case <-tick:
		}
		result, err := s.Sync(ctx, false)
		if ctx.Err() != nil {
			return
		}
		done(result, err)
	}
}
