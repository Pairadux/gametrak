package tracker

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/hyprland"
	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/session"
	"github.com/austincgause/gametrak/internal/state"
)

var start = time.Date(2026, 9, 16, 20, 0, 0, 0, time.Local)

// newTestTracker builds a tracker writing to temporary files, with
// notifications off so tests never shell out.
func newTestTracker(t *testing.T) (*Tracker, models.Settings) {
	t.Helper()

	dir := t.TempDir()
	settings := models.Settings{
		LogSessions:    true,
		MinSessionMins: 15,
		SessionsFile:   filepath.Join(dir, "sessions.jsonl"),
		StateFile:      filepath.Join(dir, "active.json"),
	}
	cfg := models.Config{
		Games: []models.Game{
			{Class: "RimWorldLinux", Name: "RimWorld"},
			{Class: "steam_app_", Name: "Steam Games", Prefix: true, UseTitle: true},
		},
		Settings: settings,
	}

	return New(cfg, io.Discard, false), settings
}

func loggedSessions(t *testing.T, settings models.Settings) []models.SessionLog {
	t.Helper()

	sessions, err := session.LoadAll(settings.SessionsFile)
	if err != nil {
		t.Fatalf("failed to load sessions: %v", err)
	}
	return sessions
}

func TestOpenAndCloseRecordsSession(t *testing.T) {
	track, settings := newTestTracker(t)

	track.HandleEvent("openwindow>>a1,1,RimWorldLinux,RimWorld", start)
	if len(track.Active()) != 1 {
		t.Fatalf("active sessions = %d, want 1", len(track.Active()))
	}

	track.HandleEvent("closewindow>>a1", start.Add(90*time.Minute))
	if len(track.Active()) != 0 {
		t.Errorf("active sessions = %d, want 0 after close", len(track.Active()))
	}

	sessions := loggedSessions(t, settings)
	if len(sessions) != 1 {
		t.Fatalf("logged %d sessions, want 1", len(sessions))
	}
	if sessions[0].Game != "RimWorld" {
		t.Errorf("game = %q, want RimWorld", sessions[0].Game)
	}
	if want := int64((90 * time.Minute).Seconds()); sessions[0].DurationSeconds != want {
		t.Errorf("duration = %d, want %d", sessions[0].DurationSeconds, want)
	}
}

func TestShortSessionIsNotLogged(t *testing.T) {
	track, settings := newTestTracker(t)

	track.HandleEvent("openwindow>>a1,1,RimWorldLinux,RimWorld", start)
	track.HandleEvent("closewindow>>a1", start.Add(5*time.Minute))

	if got := loggedSessions(t, settings); len(got) != 0 {
		t.Errorf("logged %d sessions, want none below the minimum duration", len(got))
	}
}

func TestUnmatchedWindowIsIgnored(t *testing.T) {
	track, settings := newTestTracker(t)

	track.HandleEvent("openwindow>>a1,1,firefox,Mozilla Firefox", start)
	if len(track.Active()) != 0 {
		t.Fatalf("active sessions = %d, want 0", len(track.Active()))
	}

	track.HandleEvent("closewindow>>a1", start.Add(time.Hour))
	if got := loggedSessions(t, settings); len(got) != 0 {
		t.Errorf("logged %d sessions, want none", len(got))
	}
}

func TestTitleIsSanitizedAndUsedWhenConfigured(t *testing.T) {
	track, settings := newTestTracker(t)

	track.HandleEvent("openwindow>>a1,1,steam_app_1422450,ARC\u200bRaiders", start)
	track.HandleEvent("closewindow>>a1", start.Add(time.Hour))

	sessions := loggedSessions(t, settings)
	if len(sessions) != 1 {
		t.Fatalf("logged %d sessions, want 1", len(sessions))
	}
	if sessions[0].Game != "ARCRaiders" {
		t.Errorf("game = %q, want the sanitized title", sessions[0].Game)
	}
}

func TestStateTracksActiveSessions(t *testing.T) {
	track, settings := newTestTracker(t)

	track.HandleEvent("openwindow>>a1,1,RimWorldLinux,RimWorld", start)

	saved, err := state.Load(settings.StateFile)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if len(saved.Sessions) != 1 || saved.Sessions[0].Game != "RimWorld" {
		t.Fatalf("state = %+v, want one RimWorld session", saved.Sessions)
	}

	track.HandleEvent("closewindow>>a1", start.Add(time.Hour))

	saved, err = state.Load(settings.StateFile)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if len(saved.Sessions) != 0 {
		t.Errorf("state = %+v, want no active sessions after close", saved.Sessions)
	}
}

func TestReconcileResumesStillOpenWindow(t *testing.T) {
	track, settings := newTestTracker(t)

	// A previous run was tracking RimWorld when it stopped.
	previous := []models.Session{{Address: "a1", Class: "RimWorldLinux", GameName: "RimWorld", StartTime: start}}
	if err := state.Save(settings.StateFile, previous, start.Add(time.Hour)); err != nil {
		t.Fatalf("failed to seed state: %v", err)
	}

	clients := []hyprland.Client{{Address: "a1", Class: "RimWorldLinux", Title: "RimWorld"}}
	if err := track.Reconcile(clients, start.Add(2*time.Hour)); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	active := track.Active()
	if len(active) != 1 {
		t.Fatalf("active sessions = %d, want 1", len(active))
	}
	// The original start time must survive, or the restart would cost playtime.
	if !active[0].StartTime.Equal(start) {
		t.Errorf("start = %v, want the original %v", active[0].StartTime, start)
	}
	if got := loggedSessions(t, settings); len(got) != 0 {
		t.Errorf("logged %d sessions, want none for a session still running", len(got))
	}
}

func TestReconcileRecordsSessionWhoseWindowClosed(t *testing.T) {
	track, settings := newTestTracker(t)

	previous := []models.Session{{Address: "a1", Class: "RimWorldLinux", GameName: "RimWorld", StartTime: start}}
	lastAlive := start.Add(2 * time.Hour)
	if err := state.Save(settings.StateFile, previous, lastAlive); err != nil {
		t.Fatalf("failed to seed state: %v", err)
	}

	// The window is gone, so the session ended while nothing was watching.
	if err := track.Reconcile(nil, lastAlive.Add(8*time.Hour)); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if len(track.Active()) != 0 {
		t.Errorf("active sessions = %d, want 0", len(track.Active()))
	}

	sessions := loggedSessions(t, settings)
	if len(sessions) != 1 {
		t.Fatalf("logged %d sessions, want 1", len(sessions))
	}
	// The end time is when the daemon was last known alive, not now.
	if want := int64((2 * time.Hour).Seconds()); sessions[0].DurationSeconds != want {
		t.Errorf("duration = %d, want %d", sessions[0].DurationSeconds, want)
	}
}

func TestReconcileAdoptsUntrackedGameWindow(t *testing.T) {
	track, _ := newTestTracker(t)

	clients := []hyprland.Client{
		{Address: "a1", Class: "RimWorldLinux", Title: "RimWorld"},
		{Address: "b2", Class: "firefox", Title: "Mozilla Firefox"},
	}
	if err := track.Reconcile(clients, start); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	active := track.Active()
	if len(active) != 1 {
		t.Fatalf("active sessions = %d, want only the game window", len(active))
	}
	if active[0].GameName != "RimWorld" || !active[0].StartTime.Equal(start) {
		t.Errorf("adopted session = %+v, want RimWorld starting now", active[0])
	}
}

func TestShutdownDefersRatherThanSplittingSessions(t *testing.T) {
	track, settings := newTestTracker(t)

	track.HandleEvent("openwindow>>a1,1,RimWorldLinux,RimWorld", start)
	if err := track.Shutdown(start.Add(time.Hour)); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	// Whether the game really ended is only knowable at the next startup, so
	// nothing is written to the log yet.
	if got := loggedSessions(t, settings); len(got) != 0 {
		t.Errorf("logged %d sessions, want none at shutdown", len(got))
	}

	saved, err := state.Load(settings.StateFile)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if len(saved.Sessions) != 1 {
		t.Fatalf("state = %+v, want the session preserved for the next run", saved.Sessions)
	}
	if !saved.UpdatedAt.Equal(start.Add(time.Hour)) {
		t.Errorf("updated at = %v, want the shutdown time", saved.UpdatedAt)
	}
}

func TestRestartDoesNotSplitAnOngoingSession(t *testing.T) {
	first, settings := newTestTracker(t)
	first.HandleEvent("openwindow>>a1,1,RimWorldLinux,RimWorld", start)
	if err := first.Shutdown(start.Add(time.Hour)); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	// A second run picks the session back up and sees it through to the end.
	second := New(models.Config{
		Games:    []models.Game{{Class: "RimWorldLinux", Name: "RimWorld"}},
		Settings: settings,
	}, io.Discard, false)

	clients := []hyprland.Client{{Address: "a1", Class: "RimWorldLinux", Title: "RimWorld"}}
	if err := second.Reconcile(clients, start.Add(70*time.Minute)); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	second.HandleEvent("closewindow>>a1", start.Add(3*time.Hour))

	sessions := loggedSessions(t, settings)
	if len(sessions) != 1 {
		t.Fatalf("logged %d sessions, want a single unbroken session", len(sessions))
	}
	if want := int64((3 * time.Hour).Seconds()); sessions[0].DurationSeconds != want {
		t.Errorf("duration = %d, want the full %d seconds", sessions[0].DurationSeconds, want)
	}
}
