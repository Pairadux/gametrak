package logbook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/session"
)

// fakeLogbook serves the parts of Logbook's API that syncing uses, with a
// change counter that moves on every write like the real one.
type fakeLogbook struct {
	mu       sync.Mutex
	rows     []Row
	version  int64
	nextID   int
	requests map[string]int
	// failNextCreate stores the next created row but answers 500, like a
	// response lost after the write.
	failNextCreate bool
}

func newFakeLogbook(t *testing.T) (*fakeLogbook, *httptest.Server) {
	t.Helper()
	fake := &fakeLogbook{requests: make(map[string]int)}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

func (f *fakeLogbook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	route := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v1/sheets/gaming/")
	f.requests[route]++
	switch route {
	case "GET version":
		json.NewEncoder(w).Encode(map[string]any{"version": f.version})
	case "GET rows":
		// Pages of two, to exercise the cursor.
		start, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
		end := min(start+2, len(f.rows))
		var next *string
		if end < len(f.rows) {
			cursor := strconv.Itoa(end)
			next = &cursor
		}
		json.NewEncoder(w).Encode(map[string]any{"rows": f.rows[start:end], "next_cursor": next})
	case "POST rows":
		var in NewRow
		json.NewDecoder(r.Body).Decode(&in)
		start, err := time.Parse(time.RFC3339, in.OccurredAt)
		if strings.TrimSpace(in.Game) == "" || in.Minutes < 1 || err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"validation_error","message":"Invalid fields (game: Required)"}}`))
			return
		}
		f.nextID++
		row := Row{
			ID:         fmt.Sprintf("row-%d", f.nextID),
			OccurredAt: start.UTC().Format("2006-01-02T15:04:05.000Z"),
			Data:       RowData{Game: strings.TrimSpace(in.Game), Minutes: in.Minutes, Platform: in.Platform},
		}
		f.rows = append(f.rows, row)
		f.version++
		if f.failNextCreate {
			f.failNextCreate = false
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"row": row})
	default:
		http.NotFound(w, r)
	}
}

// edit changes the sheet as the site's UI would.
func (f *fakeLogbook) edit(change func(rows []Row) []Row) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = change(f.rows)
	f.version++
}

func (f *fakeLogbook) count(route string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[route]
}

func (f *fakeLogbook) rowCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

func newTestSyncer(t *testing.T, server *httptest.Server, sessions ...models.SessionLog) *Syncer {
	t.Helper()
	dir := t.TempDir()
	syncer := &Syncer{
		Client:       NewClient(server.URL, "gaming", Credentials{ClientID: "id", ClientSecret: "secret"}),
		SessionsFile: filepath.Join(dir, "sessions.jsonl"),
		StateFile:    filepath.Join(dir, "logbook.json"),
		LockFile:     filepath.Join(dir, "logbook.lock"),
		Location:     newYork,
	}
	err := session.Update(syncer.SessionsFile, func([]models.SessionLog) ([]models.SessionLog, bool) {
		return sessions, len(sessions) > 0
	})
	if err != nil {
		t.Fatalf("failed to write sessions: %v", err)
	}
	return syncer
}

func recorded(game, start string, seconds int64) models.SessionLog {
	t, _ := time.Parse(time.RFC3339, start)
	return models.SessionLog{
		Game: game, Class: strings.ToLower(game), Start: start,
		End: t.Add(time.Duration(seconds) * time.Second).Format(time.RFC3339), DurationSeconds: seconds,
	}
}

func syncOnce(t *testing.T, syncer *Syncer, full bool) Result {
	t.Helper()
	result, err := syncer.Sync(context.Background(), full)
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	return result
}

func localSessions(t *testing.T, syncer *Syncer) []models.SessionLog {
	t.Helper()
	sessions, err := session.LoadAll(syncer.SessionsFile)
	if err != nil {
		t.Fatalf("failed to load sessions: %v", err)
	}
	return sessions
}

func TestFirstSyncUploadsHistoryAndLinksIt(t *testing.T) {
	fake, server := newFakeLogbook(t)
	syncer := newTestSyncer(t, server,
		recorded("Deadlock", "2026-10-15T20:15:03-04:00", 5727),
		recorded("RimWorld", "2026-10-14T19:00:00-04:00", 20),
		recorded("Half-Life 2", "2026-10-13T18:00:00-04:00", 3600),
	)

	result := syncOnce(t, syncer, false)
	if !result.Pulled || result.Uploaded != 3 || result.Changes.Any() {
		t.Errorf("result = %+v, want a pull with no changes and 3 uploads", result)
	}

	// Oldest first, minutes rounded and never zero, start as an instant.
	want := []RowData{{"Half-Life 2", 60, "PC"}, {"RimWorld", 1, "PC"}, {"Deadlock", 95, "PC"}}
	for i, row := range fake.rows {
		if row.Data != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, row.Data, want[i])
		}
	}
	if fake.rows[2].OccurredAt != "2026-10-16T00:15:03.000Z" {
		t.Errorf("occurred_at = %s, want the session's start in UTC", fake.rows[2].OccurredAt)
	}

	for _, s := range localSessions(t, syncer) {
		if s.LogbookID == "" || s.Platform != "PC" {
			t.Errorf("session %s = %+v, want it linked with platform PC", s.Game, s)
		}
	}
}

func TestSyncSkipsThePullWhenOnlyItsOwnUploadsChangedTheSheet(t *testing.T) {
	fake, server := newFakeLogbook(t)
	syncer := newTestSyncer(t, server, recorded("Deadlock", "2026-10-15T20:00:00-04:00", 3600))
	syncOnce(t, syncer, false)

	err := session.Log(syncer.SessionsFile, models.Session{Class: "rimworld", GameName: "RimWorld",
		StartTime: time.Date(2026, 10, 16, 20, 0, 0, 0, newYork)}, time.Date(2026, 10, 16, 21, 0, 0, 0, newYork))
	if err != nil {
		t.Fatalf("failed to log session: %v", err)
	}
	if result := syncOnce(t, syncer, false); result.Pulled || result.Uploaded != 1 {
		t.Errorf("result = %+v, want one upload and no pull", result)
	}
	if result := syncOnce(t, syncer, false); result.Pulled || result.Uploaded != 0 {
		t.Errorf("result = %+v, want nothing to do", result)
	}
	if got := fake.count("GET rows"); got != 1 {
		t.Errorf("fetched the rows %d times, want only on the first sync", got)
	}
}

func TestSiteEditsAreMirroredOnTheNextSync(t *testing.T) {
	fake, server := newFakeLogbook(t)
	syncer := newTestSyncer(t, server,
		recorded("Steam Games", "2026-10-15T20:00:00-04:00", 3600),
		recorded("RimWorld", "2026-10-16T20:00:00-04:00", 3600),
	)
	syncOnce(t, syncer, false)

	fake.edit(func(rows []Row) []Row {
		rows[0].Data.Game = "Baldur's Gate 3"
		rows[0].Data.Minutes = 75
		rows = rows[:1]
		return append(rows, Row{ID: "added", OccurredAt: "2026-10-17T15:00:00.000Z",
			Data: RowData{Game: "Zelda", Minutes: 40, Platform: "Switch"}})
	})

	result := syncOnce(t, syncer, false)
	if !result.Pulled || result.Changes != (Changes{Added: 1, Updated: 1, Deleted: 1}) {
		t.Errorf("result = %+v, want 1 added, 1 updated and 1 deleted", result)
	}

	sessions := localSessions(t, syncer)
	if len(sessions) != 2 {
		t.Fatalf("local log has %d sessions, want 2: %+v", len(sessions), sessions)
	}
	if sessions[0].Game != "Baldur's Gate 3" || sessions[0].DurationSeconds != 4500 {
		t.Errorf("edited session = %+v, want the new name and 75 minutes", sessions[0])
	}
	if sessions[1].Game != "Zelda" || sessions[1].Platform != "Switch" || sessions[1].Start != "2026-10-17T11:00:00-04:00" {
		t.Errorf("added session = %+v, want Zelda on the Switch at 11am New York time", sessions[1])
	}
}

func TestAnUploadWhoseResponseWasLostIsLinkedNotDuplicated(t *testing.T) {
	fake, server := newFakeLogbook(t)
	syncer := newTestSyncer(t, server, recorded("Deadlock", "2026-10-15T20:00:00-04:00", 3600))
	fake.failNextCreate = true

	if _, err := syncer.Sync(context.Background(), false); err == nil || !Temporary(err) {
		t.Fatalf("Sync error = %v, want a temporary error", err)
	}
	if s := localSessions(t, syncer)[0]; s.LogbookID != "" {
		t.Fatalf("session linked to %q without an answer", s.LogbookID)
	}

	result := syncOnce(t, syncer, false)
	if result.Changes.Linked != 1 || result.Uploaded != 0 {
		t.Errorf("result = %+v, want the existing row linked and nothing uploaded", result)
	}
	if fake.rowCount() != 1 {
		t.Errorf("sheet has %d rows, want 1", fake.rowCount())
	}
	if s := localSessions(t, syncer)[0]; s.LogbookID != "row-1" {
		t.Errorf("session = %+v, want it linked to row-1", s)
	}
}

func TestRejectedSessionsWaitForAFullSync(t *testing.T) {
	fake, server := newFakeLogbook(t)
	syncer := newTestSyncer(t, server, recorded("   ", "2026-10-15T20:00:00-04:00", 3600))

	if result := syncOnce(t, syncer, false); result.Rejected != 1 || result.Uploaded != 0 {
		t.Errorf("result = %+v, want one rejected session", result)
	}
	syncOnce(t, syncer, false)
	if got := fake.count("POST rows"); got != 1 {
		t.Errorf("posted %d times, want the rejected session skipped by the second sync", got)
	}
	if result := syncOnce(t, syncer, true); result.Rejected != 1 {
		t.Errorf("result = %+v, want a full sync to try it again", result)
	}
	if got := fake.count("POST rows"); got != 2 {
		t.Errorf("posted %d times, want 2", got)
	}
}

func threeSessions() []models.SessionLog {
	return []models.SessionLog{
		recorded("Deadlock", "2026-10-15T20:00:00-04:00", 3600),
		recorded("RimWorld", "2026-10-16T20:00:00-04:00", 3600),
		recorded("Half-Life 2", "2026-10-17T20:00:00-04:00", 3600),
	}
}

func TestAnEmptySheetDoesNotWipeLinkedSessions(t *testing.T) {
	fake, server := newFakeLogbook(t)
	syncer := newTestSyncer(t, server, threeSessions()...)
	syncOnce(t, syncer, false)

	fake.edit(func([]Row) []Row { return nil })
	if _, err := syncer.Sync(context.Background(), false); err == nil || !strings.Contains(err.Error(), "not deleting") {
		t.Errorf("Sync error = %v, want a refusal to delete", err)
	}
	if got := localSessions(t, syncer); len(got) != 3 || linkedCount(got) != 3 {
		t.Errorf("local log = %+v, want the linked sessions kept", got)
	}
}

func TestASheetWithNoneOfTheLinkedRowsIsNotMirrored(t *testing.T) {
	_, server := newFakeLogbook(t)
	syncer := newTestSyncer(t, server, threeSessions()...)
	syncOnce(t, syncer, false)

	// Another site with rows of its own, and no record of which site the
	// log's ids came from.
	other, otherServer := newFakeLogbook(t)
	other.edit(func([]Row) []Row {
		return []Row{{ID: "theirs", OccurredAt: "2026-10-01T12:00:00.000Z", Data: RowData{Game: "Zelda", Minutes: 30, Platform: "Switch"}}}
	})
	os.Remove(syncer.StateFile)
	syncer.Client = NewClient(otherServer.URL, "gaming", Credentials{})

	if _, err := syncer.Sync(context.Background(), false); err == nil || !strings.Contains(err.Error(), "none of the 3 rows") {
		t.Errorf("Sync error = %v, want a refusal to mirror", err)
	}
	if got := localSessions(t, syncer); len(got) != 3 || linkedCount(got) != 3 {
		t.Errorf("local log = %+v, want it untouched", got)
	}
}

func TestDeletingTheOnlySessionsOnTheSiteStillWorks(t *testing.T) {
	fake, server := newFakeLogbook(t)
	syncer := newTestSyncer(t, server, recorded("Deadlock", "2026-10-15T20:00:00-04:00", 3600))
	syncOnce(t, syncer, false)

	fake.edit(func([]Row) []Row { return nil })
	if result := syncOnce(t, syncer, false); result.Changes.Deleted != 1 {
		t.Errorf("result = %+v, want the session deleted", result)
	}
	if got := localSessions(t, syncer); len(got) != 0 {
		t.Errorf("local log = %+v, want it empty", got)
	}
}

func TestLinkedSessionsAreNotMirroredFromAnotherSite(t *testing.T) {
	_, server := newFakeLogbook(t)
	syncer := newTestSyncer(t, server, recorded("Deadlock", "2026-10-15T20:00:00-04:00", 3600))
	syncOnce(t, syncer, false)

	_, other := newFakeLogbook(t)
	syncer.Client = NewClient(other.URL, "gaming", Credentials{})
	if _, err := syncer.Sync(context.Background(), false); err == nil || !strings.Contains(err.Error(), "linked to rows on") {
		t.Errorf("Sync error = %v, want a refusal to mirror another site", err)
	}
	if got := localSessions(t, syncer); len(got) != 1 {
		t.Errorf("local log = %+v, want it untouched", got)
	}
}

func TestRunSyncsOnTriggersUntilCancelled(t *testing.T) {
	fake, server := newFakeLogbook(t)
	syncer := newTestSyncer(t, server, recorded("Deadlock", "2026-10-15T20:00:00-04:00", 3600))

	ctx, cancel := context.WithCancel(context.Background())
	trigger := make(chan struct{}, 1)
	results := make(chan Result)
	stopped := make(chan struct{})
	go func() {
		syncer.Run(ctx, trigger, func(r Result, err error) {
			if err != nil {
				t.Errorf("sync failed: %v", err)
			}
			results <- r
		})
		close(stopped)
	}()

	trigger <- struct{}{}
	if r := <-results; r.Uploaded != 1 {
		t.Errorf("result = %+v, want one upload", r)
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if fake.rowCount() != 1 {
		t.Errorf("sheet has %d rows, want 1", fake.rowCount())
	}
	if _, err := os.Stat(syncer.StateFile); err != nil {
		t.Errorf("no sync state written: %v", err)
	}
}
