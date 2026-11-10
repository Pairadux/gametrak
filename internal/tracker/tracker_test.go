package tracker

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/session"
	"github.com/austincgause/gametrak/internal/source"
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

	track := New(cfg, io.Discard, false)
	track.UseClock(&fakeClock{boot: "boot-1"})
	return track, settings
}

// fakeClock stands in for the kernel's sleep counter: tests move asleep
// forward to simulate the machine being suspended.
type fakeClock struct {
	asleep time.Duration
	boot   string
}

func (c *fakeClock) Asleep() time.Duration { return c.asleep }
func (c *fakeClock) Boot() string          { return c.boot }

func window(id, class, title string) source.Window {
	return source.Window{ID: id, Class: class, Title: title}
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

	track.Opened(window("a1", "RimWorldLinux", "RimWorld"), start)
	if len(track.Active()) != 1 {
		t.Fatalf("active sessions = %d, want 1", len(track.Active()))
	}

	track.Closed("a1", start.Add(90*time.Minute))
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

	track.Opened(window("a1", "RimWorldLinux", "RimWorld"), start)
	track.Closed("a1", start.Add(5*time.Minute))

	if got := loggedSessions(t, settings); len(got) != 0 {
		t.Errorf("logged %d sessions, want none below the minimum duration", len(got))
	}
}

func TestOnLoggedRunsOnlyForSessionsWrittenToTheLog(t *testing.T) {
	track, _ := newTestTracker(t)
	calls := 0
	track.OnLogged(func() { calls++ })

	track.Opened(window("a1", "RimWorldLinux", "RimWorld"), start)
	track.Closed("a1", start.Add(5*time.Minute))
	if calls != 0 {
		t.Errorf("OnLogged ran %d times for a session too short to log", calls)
	}

	track.Opened(window("a2", "RimWorldLinux", "RimWorld"), start)
	track.Closed("a2", start.Add(90*time.Minute))
	if calls != 1 {
		t.Errorf("OnLogged ran %d times, want once for the logged session", calls)
	}
}

func TestUnmatchedWindowIsIgnored(t *testing.T) {
	track, settings := newTestTracker(t)

	track.Opened(window("a1", "firefox", "Mozilla Firefox"), start)
	if len(track.Active()) != 0 {
		t.Fatalf("active sessions = %d, want 0", len(track.Active()))
	}

	track.Closed("a1", start.Add(time.Hour))
	if got := loggedSessions(t, settings); len(got) != 0 {
		t.Errorf("logged %d sessions, want none", len(got))
	}
}

func TestTitleIsSanitizedAndUsedWhenConfigured(t *testing.T) {
	track, settings := newTestTracker(t)

	track.Opened(window("a1", "steam_app_1422450", "ARC\u200bRaiders"), start)
	track.Closed("a1", start.Add(time.Hour))

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

	track.Opened(window("a1", "RimWorldLinux", "RimWorld"), start)

	saved, err := state.Load(settings.StateFile)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if len(saved.Sessions) != 1 || saved.Sessions[0].Game != "RimWorld" {
		t.Fatalf("state = %+v, want one RimWorld session", saved.Sessions)
	}

	track.Closed("a1", start.Add(time.Hour))

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

	open := []source.Window{window("a1", "RimWorldLinux", "RimWorld")}
	if err := track.Reconcile(open, start.Add(2*time.Hour)); err != nil {
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

	open := []source.Window{
		window("a1", "RimWorldLinux", "RimWorld"),
		window("b2", "firefox", "Mozilla Firefox"),
	}
	if err := track.Reconcile(open, start); err != nil {
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

	track.Opened(window("a1", "RimWorldLinux", "RimWorld"), start)
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
	first.Opened(window("a1", "RimWorldLinux", "RimWorld"), start)
	if err := first.Shutdown(start.Add(time.Hour)); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	// A second run picks the session back up and sees it through to the end.
	second := New(models.Config{
		Games:    []models.Game{{Class: "RimWorldLinux", Name: "RimWorld"}},
		Settings: settings,
	}, io.Discard, false)
	second.UseClock(&fakeClock{boot: "boot-1"})

	open := []source.Window{window("a1", "RimWorldLinux", "RimWorld")}
	if err := second.Reconcile(open, start.Add(70*time.Minute)); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	second.Closed("a1", start.Add(3*time.Hour))

	sessions := loggedSessions(t, settings)
	if len(sessions) != 1 {
		t.Fatalf("logged %d sessions, want a single unbroken session", len(sessions))
	}
	if want := int64((3 * time.Hour).Seconds()); sessions[0].DurationSeconds != want {
		t.Errorf("duration = %d, want the full %d seconds", sessions[0].DurationSeconds, want)
	}
}

func TestNamesPreferAnExactConfigEntryThenTheSourcesNameThenTheTitle(t *testing.T) {
	track, _ := newTestTracker(t)
	track.cfg.Games = append(track.cfg.Games, models.Game{Class: "steam_app_1086940", Name: "BG3"})

	steam := func(id, class, title, name string) source.Window {
		return source.Window{ID: id, Class: class, Title: title, Name: name}
	}
	track.Opened(steam("a1", "steam_app_1086940", "Baldur's Gate 3 (v4.1)", "Baldur's Gate 3"), start)
	track.Opened(steam("a2", "steam_app_1145360", "Hades II  ", "Hades II"), start)
	track.Opened(steam("a3", "steam_app_1422450", "ARC Raiders", ""), start)
	track.Opened(steam("a4", "RimWorldLinux", "RimWorld by Ludeon", ""), start)
	track.Opened(source.Window{ID: "steam:1127500", Class: "steam_app_1127500", Name: "Mini Motorways", Game: true}, start)
	track.Opened(source.Window{ID: "x", Class: "unknown", Title: "Some Game", Game: true}, start)
	track.Opened(steam("a5", "firefox", "Mozilla Firefox", ""), start)

	want := map[string]string{
		"a1":            "BG3",            // an exact class in the config wins
		"a2":            "Hades II",       // Steam's name beats the window title
		"a3":            "ARC Raiders",    // no Steam name: the title, as configured
		"a4":            "RimWorld",       // a native game keeps its configured name
		"steam:1127500": "Mini Motorways", // Steam source games need no config entry
		"x":             "Some Game",
	}
	active := track.Active()
	if len(active) != len(want) {
		t.Fatalf("active = %+v, want %d sessions and no Firefox", active, len(want))
	}
	for _, s := range active {
		if s.GameName != want[s.Address] {
			t.Errorf("%s named %q, want %q", s.Address, s.GameName, want[s.Address])
		}
	}
}

func TestOpeningATrackedWindowAgainKeepsItsStart(t *testing.T) {
	track, _ := newTestTracker(t)
	w := source.Window{ID: "steam:1", Class: "steam_app_1", Name: "Game", Game: true}

	track.Opened(w, start)
	track.Opened(w, start.Add(time.Hour))
	if active := track.Active(); len(active) != 1 || !active[0].StartTime.Equal(start) {
		t.Errorf("active = %+v, want one session from the first open", active)
	}
}

func newSleepyTracker(t *testing.T) (*Tracker, models.Settings, *fakeClock) {
	t.Helper()
	track, settings := newTestTracker(t)
	clock := &fakeClock{asleep: 7 * time.Minute, boot: "boot-1"}
	track.UseClock(clock)
	return track, settings, clock
}

func TestTimeAsleepIsNotCountedAsPlayed(t *testing.T) {
	track, settings, clock := newSleepyTracker(t)

	track.Opened(window("a1", "RimWorldLinux", "RimWorld"), start)
	clock.asleep += 30 * time.Minute // the machine sleeps for half an hour mid-game
	track.Closed("a1", start.Add(90*time.Minute))

	sessions := loggedSessions(t, settings)
	if len(sessions) != 1 {
		t.Fatalf("logged %d sessions, want 1", len(sessions))
	}
	if want := int64((60 * time.Minute).Seconds()); sessions[0].DurationSeconds != want {
		t.Errorf("duration = %d, want %d (90 minutes less 30 asleep)", sessions[0].DurationSeconds, want)
	}
	// Start and end are still the real times the game opened and closed.
	if end, _ := sessions[0].EndTime(); !end.Equal(start.Add(90 * time.Minute)) {
		t.Errorf("end = %v, want %v", end, start.Add(90*time.Minute))
	}
}

func TestTheMinimumAppliesToTimePlayedNotTimeAsleep(t *testing.T) {
	track, settings, clock := newSleepyTracker(t)

	track.Opened(window("a1", "RimWorldLinux", "RimWorld"), start)
	clock.asleep += time.Hour
	track.Closed("a1", start.Add(70*time.Minute)) // 10 minutes played, under the 15 minimum

	if got := loggedSessions(t, settings); len(got) != 0 {
		t.Errorf("logged %+v, want nothing: only 10 minutes were played", got)
	}
}

func TestSleepCountedBeforeARestartIsKept(t *testing.T) {
	first, settings, clock := newSleepyTracker(t)
	first.Opened(window("a1", "RimWorldLinux", "RimWorld"), start)
	clock.asleep += 20 * time.Minute
	if err := first.Shutdown(start.Add(time.Hour)); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	saved, _ := state.Load(settings.StateFile)
	if len(saved.Sessions) != 1 || saved.Sessions[0].Paused != 20*time.Minute {
		t.Fatalf("state = %+v, want the 20 minutes asleep saved", saved.Sessions)
	}

	// Same boot: the next run picks the session up and keeps counting sleep.
	second := New(first.cfg, io.Discard, false)
	second.UseClock(clock)
	if err := second.Reconcile([]source.Window{window("a1", "RimWorldLinux", "RimWorld")}, start.Add(61*time.Minute)); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	clock.asleep += 10 * time.Minute
	second.Closed("a1", start.Add(2*time.Hour))

	sessions := loggedSessions(t, settings)
	if len(sessions) != 1 {
		t.Fatalf("logged %d sessions, want 1", len(sessions))
	}
	if want := int64((90 * time.Minute).Seconds()); sessions[0].DurationSeconds != want {
		t.Errorf("duration = %d, want %d (2 hours less 30 minutes asleep)", sessions[0].DurationSeconds, want)
	}
}

func TestARebootEndsTheSessionEvenIfTheSameIDIsOpenAgain(t *testing.T) {
	first, settings, clock := newSleepyTracker(t)
	game := source.Window{ID: "steam:1127500", Class: "steam_app_1127500", Name: "Mini Motorways", Game: true}
	first.Opened(game, start)
	clock.asleep += 5 * time.Minute
	if err := first.Persist(start.Add(40 * time.Minute)); err != nil {
		t.Fatalf("Persist returned error: %v", err)
	}

	// After a reboot the counter starts over, and the game was relaunched.
	second := New(first.cfg, io.Discard, false)
	second.UseClock(&fakeClock{boot: "boot-2"})
	if err := second.Reconcile([]source.Window{game}, start.Add(3*time.Hour)); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	sessions := loggedSessions(t, settings)
	if len(sessions) != 1 {
		t.Fatalf("logged %d sessions, want the one from before the reboot", len(sessions))
	}
	// It ended at the last state write, less the sleep counted by then.
	if want := int64((35 * time.Minute).Seconds()); sessions[0].DurationSeconds != want {
		t.Errorf("duration = %d, want %d", sessions[0].DurationSeconds, want)
	}
	if active := second.Active(); len(active) != 1 || !active[0].StartTime.Equal(start.Add(3*time.Hour)) {
		t.Errorf("active = %+v, want a new session starting now", active)
	}
}

func TestStateFromBeforeSleepTrackingStillResumes(t *testing.T) {
	track, settings, clock := newSleepyTracker(t)
	// An older gametrak wrote this state, with no boot or sleep fields.
	old := []models.Session{{Address: "a1", Class: "RimWorldLinux", GameName: "RimWorld", StartTime: start}}
	if err := state.Save(settings.StateFile, old, start.Add(time.Hour)); err != nil {
		t.Fatalf("failed to seed state: %v", err)
	}

	if err := track.Reconcile([]source.Window{window("a1", "RimWorldLinux", "RimWorld")}, start.Add(time.Hour)); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	clock.asleep += 15 * time.Minute
	track.Closed("a1", start.Add(2*time.Hour))

	sessions := loggedSessions(t, settings)
	if len(sessions) != 1 {
		t.Fatalf("logged %d sessions, want 1", len(sessions))
	}
	if want := int64((105 * time.Minute).Seconds()); sessions[0].DurationSeconds != want {
		t.Errorf("duration = %d, want %d (sleep counted from the resume on)", sessions[0].DurationSeconds, want)
	}
}
