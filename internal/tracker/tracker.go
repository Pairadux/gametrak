// Package tracker turns Hyprland window events into recorded game sessions.
package tracker

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/austincgause/gametrak/internal/hyprland"
	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/notify"
	"github.com/austincgause/gametrak/internal/session"
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
}

// New creates a tracker that reports activity to out.
func New(cfg models.Config, out io.Writer, debug bool) *Tracker {
	return &Tracker{
		cfg:      cfg,
		out:      out,
		debug:    debug,
		sessions: make(map[string]*models.Session),
	}
}

// Active returns the in-flight sessions, oldest first.
func (t *Tracker) Active() []models.Session {
	sessions := make([]models.Session, 0, len(t.sessions))
	for _, s := range t.sessions {
		sessions = append(sessions, *s)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].StartTime.Before(sessions[j].StartTime) })
	return sessions
}

// Reconcile aligns the tracker with reality at startup. Sessions recorded by a
// previous run are resumed when their window is still open, and recorded as
// ended when it is not. Game windows that were already open but never tracked
// are adopted starting now.
func (t *Tracker) Reconcile(clients []hyprland.Client, now time.Time) error {
	previous, err := state.Load(t.cfg.Settings.StateFile)
	if err != nil {
		return err
	}

	untracked := make(map[string]hyprland.Client)
	for _, client := range clients {
		if _, matched := utility.MatchGame(client.Class, t.cfg.Games); matched {
			untracked[client.Address] = client
		}
	}

	for _, active := range previous.Sessions {
		if _, stillOpen := untracked[active.Address]; stillOpen {
			resumed := active.Session()
			t.sessions[resumed.Address] = &resumed
			delete(untracked, active.Address)
			t.logf("Resumed session: %s (running since %s)", resumed.GameName, resumed.StartTime.Format("2006-01-02 15:04"))
			continue
		}

		// The window is gone, so the session ended while gametrak was not
		// running. The last state write is the best estimate of when.
		end := previous.UpdatedAt
		if end.Before(active.Start) {
			end = active.Start
		}
		t.finish(active.Session(), end, "Recovered session", false)
	}

	for _, client := range untracked {
		t.track(client.Address, client.Class, client.Title, now)
	}

	return t.Persist(now)
}

// HandleEvent processes one raw line from the Hyprland event socket.
func (t *Tracker) HandleEvent(line string, now time.Time) {
	if t.debug {
		t.logf("DEBUG: %s", line)
	}

	eventType, data, ok := hyprland.ParseEvent(line)
	if !ok {
		return
	}

	switch eventType {
	case hyprland.EventOpenWindow:
		if event, ok := hyprland.ParseOpenWindow(data); ok {
			t.openWindow(event, now)
		}
	case hyprland.EventCloseWindow:
		if event, ok := hyprland.ParseCloseWindow(data); ok {
			t.closeWindow(event, now)
		}
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

func (t *Tracker) openWindow(event hyprland.OpenWindowEvent, now time.Time) {
	if _, matched := utility.MatchGame(event.Class, t.cfg.Games); !matched {
		return
	}
	t.track(event.Address, event.Class, event.Title, now)
	if err := t.Persist(now); err != nil {
		t.warnf("failed to save state: %v", err)
	}
}

func (t *Tracker) closeWindow(event hyprland.CloseWindowEvent, now time.Time) {
	sess, exists := t.sessions[event.Address]
	if !exists {
		return
	}
	delete(t.sessions, event.Address)

	t.finish(*sess, now, "Game ended", t.cfg.Settings.Notifications)
	if err := t.Persist(now); err != nil {
		t.warnf("failed to save state: %v", err)
	}
}

// track begins a session for a matched game window.
func (t *Tracker) track(address, class, title string, now time.Time) {
	game, matched := utility.MatchGame(class, t.cfg.Games)
	if !matched {
		return
	}

	// Titles arrive from Hyprland with invisible Unicode characters baked in,
	// so sanitize once at the boundary rather than at every display site.
	title = utility.SanitizeTitle(title)

	name := game.DisplayName()
	if game.UseTitle && title != "" {
		name = title
	}

	t.sessions[address] = &models.Session{
		Address:   address,
		Class:     class,
		Title:     title,
		GameName:  name,
		StartTime: now,
	}
	t.logf("Game started: %s (class: %s, address: %s)", name, class, address)
}

// finish reports a completed session and logs it when it is long enough to keep.
func (t *Tracker) finish(sess models.Session, end time.Time, label string, notifyUser bool) {
	duration := end.Sub(sess.StartTime)
	t.logf("%s: %s - Session: %s", label, sess.GameName, utility.FormatDurationExact(duration))

	minimum := time.Duration(t.cfg.Settings.MinSessionMins) * time.Minute
	switch {
	case !t.cfg.Settings.LogSessions:
	case duration < minimum:
		t.logf("Session too short to log (min: %d mins)", t.cfg.Settings.MinSessionMins)
	default:
		if err := session.Log(t.cfg.Settings.SessionsFile, sess, end); err != nil {
			t.warnf("failed to log session: %v", err)
		}
	}

	if notifyUser {
		if err := notify.GameEnded(sess.GameName, duration); err != nil && t.debug {
			t.warnf("failed to send notification: %v", err)
		}
	}
}

func (t *Tracker) logf(format string, args ...any) {
	fmt.Fprintf(t.out, "[%s] %s\n", utility.Timestamp(), fmt.Sprintf(format, args...))
}

func (t *Tracker) warnf(format string, args ...any) {
	t.logf("Warning: "+format, args...)
}
