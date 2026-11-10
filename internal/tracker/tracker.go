// Package tracker turns game windows opening and closing into recorded game
// sessions. It doesn't know where the windows come from: Hyprland and Steam
// each have a source that reports them.
package tracker

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/notify"
	"github.com/austincgause/gametrak/internal/session"
	"github.com/austincgause/gametrak/internal/sleepclock"
	"github.com/austincgause/gametrak/internal/source"
	"github.com/austincgause/gametrak/internal/state"
	"github.com/austincgause/gametrak/internal/utility"
)

// Tracker owns the set of in-flight sessions and decides when one is recorded.
// It is not safe for concurrent use; drive it from a single event loop.
type Tracker struct {
	cfg      models.Config
	out      io.Writer
	debug    bool
	sessions map[string]*models.Session
	logged   func()
	clock    sleepclock.Clock
}

// New creates a tracker that reports activity to out.
func New(cfg models.Config, out io.Writer, debug bool) *Tracker {
	return &Tracker{
		cfg:      cfg,
		out:      out,
		debug:    debug,
		sessions: make(map[string]*models.Session),
		clock:    sleepclock.System{},
	}
}

// UseClock replaces the clock that measures time asleep, for tests.
func (t *Tracker) UseClock(clock sleepclock.Clock) {
	t.clock = clock
}

// OnLogged sets a function to call after each session is written to the log,
// such as a trigger to upload it. It runs on the caller's goroutine, so it
// must not block.
func (t *Tracker) OnLogged(f func()) {
	t.logged = f
}

// Active returns the in-flight sessions, oldest first, with the time asleep
// so far in Paused.
func (t *Tracker) Active() []models.Session {
	sessions := make([]models.Session, 0, len(t.sessions))
	for _, s := range t.sessions {
		active := *s
		active.Paused = t.paused(*s)
		sessions = append(sessions, active)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].StartTime.Before(sessions[j].StartTime) })
	return sessions
}

// Reconcile aligns the tracker with reality at startup. Sessions recorded by a
// previous run are resumed when their window is still open, and recorded as
// ended when it is not. Game windows that were already open but never tracked
// are adopted starting now.
func (t *Tracker) Reconcile(open []source.Window, now time.Time) error {
	previous, err := state.Load(t.cfg.Settings.StateFile)
	if err != nil {
		return err
	}

	untracked := make(map[string]source.Window)
	for _, w := range open {
		if _, ok := t.gameName(w); ok {
			untracked[w.ID] = w
		}
	}

	boot := t.clock.Boot()
	for _, active := range previous.Sessions {
		// A reboot always ends a game, so a session from an earlier boot is
		// over even if a window with the same ID is open now.
		sameBoot := active.Boot == "" || active.Boot == boot
		if _, stillOpen := untracked[active.Address]; stillOpen && sameBoot {
			resumed := active.Session()
			if active.Boot == "" {
				// Written before sleep was tracked: count sleep from now on.
				resumed.Boot, resumed.AsleepAtStart = boot, t.clock.Asleep()
			}
			t.sessions[resumed.Address] = &resumed
			delete(untracked, active.Address)
			t.logf("Resumed session: %s (running since %s)", resumed.GameName, resumed.StartTime.Format("2006-01-02 15:04"))
			continue
		}

		// The window is gone, so the session ended while gametrak was not
		// running. The last state write is the best estimate of when, and the
		// sleep it had counted by then is what to subtract.
		end := previous.UpdatedAt
		if end.Before(active.Start) {
			end = active.Start
		}
		ended := active.Session()
		t.finish(ended, end, active.Paused, "Recovered session", false)
	}

	for _, w := range untracked {
		t.track(w, now)
	}

	return t.Persist(now)
}

// Apply processes one event from a source.
func (t *Tracker) Apply(event source.Event, now time.Time) {
	switch event.Kind {
	case source.Opened:
		t.Opened(event.Window, now)
	case source.Closed:
		t.Closed(event.Window.ID, now)
	}
}

// Opened starts a session if the window is a game.
func (t *Tracker) Opened(w source.Window, now time.Time) {
	if _, tracked := t.sessions[w.ID]; tracked {
		return
	}
	if !t.track(w, now) {
		return
	}
	if err := t.Persist(now); err != nil {
		t.warnf("failed to save state: %v", err)
	}
}

// Closed ends the session for the window with this ID, if there is one.
func (t *Tracker) Closed(id string, now time.Time) {
	sess, exists := t.sessions[id]
	if !exists {
		return
	}
	delete(t.sessions, id)

	t.finish(*sess, now, t.paused(*sess), "Game ended", t.cfg.Settings.Notifications)
	if err := t.Persist(now); err != nil {
		t.warnf("failed to save state: %v", err)
	}
}

// Persist writes the in-flight sessions to the state file so a later run can
// resume or recover them.
func (t *Tracker) Persist(now time.Time) error {
	return state.Save(t.cfg.Settings.StateFile, t.Active(), now)
}

// Shutdown records the in-flight sessions as pending rather than ending them.
// Whether the game actually stopped is only knowable at the next startup, when
// the window list says whether it is still open.
func (t *Tracker) Shutdown(now time.Time) error {
	if len(t.sessions) > 0 {
		t.logf("%d session(s) still active; they will be recorded when gametrak next starts", len(t.sessions))
	}
	return t.Persist(now)
}

// gameName decides whether a window is a game and what to call it:
//  1. a config entry for exactly this class with a name, which is a
//     deliberate choice and wins;
//  2. the game's proper name from the source (Steam's name for the app);
//  3. the window title, for config entries that ask for it;
//  4. the config entry's name, or the class.
func (t *Tracker) gameName(w source.Window) (string, bool) {
	game, matched := utility.MatchGame(w.Class, t.cfg.Games)
	if !matched && !w.Game {
		return "", false
	}
	// Titles arrive from Hyprland with invisible Unicode characters baked in,
	// so sanitize once at the boundary rather than at every display site.
	title := utility.SanitizeTitle(w.Title)
	proper := utility.SanitizeTitle(w.Name)
	switch {
	case matched && !game.Prefix && game.Name != "":
		return game.Name, true
	case proper != "":
		return proper, true
	case matched && game.UseTitle && title != "":
		return title, true
	case matched:
		return game.DisplayName(), true
	case title != "":
		return title, true
	}
	return w.Class, true
}

// track begins a session for a game window, and reports whether it was one.
func (t *Tracker) track(w source.Window, now time.Time) bool {
	name, ok := t.gameName(w)
	if !ok {
		return false
	}

	t.sessions[w.ID] = &models.Session{
		Address:   w.ID,
		Class:     w.Class,
		Title:     utility.SanitizeTitle(w.Title),
		GameName:  name,
		StartTime: wall(now),
		Boot:      t.clock.Boot(),
		// What the sleep counter reads now; whatever it gains before the
		// session ends is time asleep.
		AsleepAtStart: t.clock.Asleep(),
	}
	t.logf("Game started: %s (class: %s, id: %s)", name, w.Class, w.ID)
	return true
}

// paused is how long the machine has slept since the session started.
func (t *Tracker) paused(s models.Session) time.Duration {
	return sleepclock.Slept(t.clock, s.Boot, s.AsleepAtStart, s.Paused)
}

// finish reports a completed session and logs it when the time played, which
// leaves out the time asleep, is long enough to keep.
func (t *Tracker) finish(sess models.Session, end time.Time, paused time.Duration, label string, notifyUser bool) {
	end = wall(end)
	sess.Paused = paused
	played := sess.Played(end)
	if paused > 0 {
		t.logf("%s: %s - Session: %s (not counting %s asleep)", label, sess.GameName,
			utility.FormatDurationExact(played), utility.FormatDurationExact(paused))
	} else {
		t.logf("%s: %s - Session: %s", label, sess.GameName, utility.FormatDurationExact(played))
	}

	minimum := time.Duration(t.cfg.Settings.MinSessionMins) * time.Minute
	switch {
	case !t.cfg.Settings.LogSessions:
	case played < minimum:
		t.logf("Session too short to log (min: %d mins)", t.cfg.Settings.MinSessionMins)
	default:
		if err := session.Log(t.cfg.Settings.SessionsFile, sess, end); err != nil {
			t.warnf("failed to log session: %v", err)
		} else if t.logged != nil {
			t.logged()
		}
	}

	if notifyUser {
		// notify-send can stall without a notification daemon (Steam's Game
		// Mode has none), so it never runs on the event loop.
		go func() {
			if err := notify.GameEnded(sess.GameName, played); err != nil && t.debug {
				t.warnf("failed to send notification: %v", err)
			}
		}()
	}
}

// wall drops a time's monotonic clock reading. Durations between times that
// carry one leave out time asleep on Linux, but sleep is subtracted explicitly
// (see sleepclock), so every duration here must be plain wall-clock time.
func wall(t time.Time) time.Time {
	return t.Round(0)
}

func (t *Tracker) logf(format string, args ...any) {
	fmt.Fprintf(t.out, "[%s] %s\n", utility.Timestamp(), fmt.Sprintf(format, args...))
}

func (t *Tracker) warnf(format string, args ...any) {
	t.logf("Warning: "+format, args...)
}
