package steam

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/austincgause/gametrak/internal/source"
)

// Source reports the Steam games that start and stop on this machine, from
// the log the Steam client keeps of the game processes it tracks:
//
//   - a game starts at its first "adding PID" line, and stops at
//     "Remove <id> from running list";
//   - only games installed here count, so non-Steam shortcuts, Remote Play
//     streams and tools are left out;
//   - the log is watched for changes (inotify), never polled;
//   - games already running when gametrak starts are found from the process
//     list, where Steam's reaper carries "SteamLaunch AppId=<id>";
//   - each running game's reaper is also watched (pidfd): its exit ends the
//     game, usually a moment ahead of Steam's line, and even if Steam crashes
//     and never logs it;
//   - with OnlyAccount, only games launched by that Steam account count. A
//     game is reported once its launch processes say whose it is, which is a
//     moment after Steam logs its first process (see judge).
type Source struct {
	root    string
	logPath string
	library *Library
	follow  *follower
	watcher *fsnotify.Watcher
	procDir string
	loc     *time.Location
	logf    func(format string, args ...any)
	debugf  func(format string, args ...any)
	account uint32 // 0 counts every account's games
	// settleAfter is how long a game may go without its processes showing
	// whose it is before it's decided with what's there.
	settleAfter time.Duration

	running     map[uint64]*runningGame
	gen         int
	exits       chan exitNotice
	settles     chan exitNotice
	stopped     chan struct{}
	stopOnce    bool
	pidfdWarned bool
}

// verdict is whether a running game counts.
type verdict int

const (
	// undecided: its launch processes don't show whose game it is yet.
	// Nothing has been reported for it.
	undecided verdict = iota
	// counted: reported as opened, and to be reported closed.
	counted
	// skipped: it doesn't count (another account's, or nobody can tell
	// whose). It's followed all the same, silently, so its later lines and
	// its end are recognized.
	skipped
)

type runningGame struct {
	name    string
	since   time.Time  // when its first process was logged
	gen     int        // tells this run of the game, and this watch, from an earlier one
	watch   *exitWatch // nil when the processes can't be watched
	verdict verdict
	// pids are the processes Steam has logged for the game while it's
	// undecided, and settle decides it if none of them ever says whose it is.
	pids   []int
	settle *time.Timer
	// onReaper is set once the watch is on the game's reaper, which lives
	// exactly as long as the game. Until the reaper is up, the watch is on
	// the first process Steam logged.
	onReaper bool
}

// exitNotice names a run of a game: to say every watched process of it has
// exited, or that it's time to settle whether it counts.
type exitNotice struct {
	appID uint64
	gen   int
}

// Open starts watching the game log in Steam's data folder root. logf reports
// what the daemon should log; debugf, which may be nil, reports more detail.
func Open(root string, logf, debugf func(format string, args ...any)) (*Source, error) {
	logPath := GameLogPath(root)
	s := &Source{
		root:    root,
		logPath: logPath,
		library: NewLibrary(root),
		// Taking the log's end before watching it means a line written in
		// between is read by Run's first read rather than lost.
		follow:      newFollower(logPath),
		procDir:     "/proc",
		loc:         time.Local,
		logf:        logf,
		debugf:      debugf,
		settleAfter: 5 * time.Second,
		running:     map[uint64]*runningGame{},
		exits:       make(chan exitNotice, 16),
		settles:     make(chan exitNotice, 16),
		stopped:     make(chan struct{}),
	}
	if s.debugf == nil {
		s.debugf = func(string, ...any) {}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("can't watch Steam's game log: %w", err)
	}
	if err := watcher.Add(filepath.Dir(logPath)); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("can't watch Steam's log folder %s: %w", filepath.Dir(logPath), err)
	}
	s.watcher = watcher
	return s, nil
}

// Name describes the source in the daemon's log.
func (s *Source) Name() string { return "Steam" }

// OnlyAccount limits the games that count to those launched by the Steam
// account with this ID. Call it before Snapshot.
func (s *Source) OnlyAccount(id uint32) {
	s.account = id
}

// Installed reports how many installed Steam games the source can see.
func (s *Source) Installed() int { return s.library.Count() }

// Snapshot lists the installed Steam games running right now, and starts
// following each of them. Call it once, before Run.
func (s *Source) Snapshot() ([]source.Window, error) {
	var windows []source.Window
	for appID, pids := range gameProcesses(s.procDir) {
		name, ok := s.library.Name(appID)
		if !ok {
			continue
		}
		game, running := s.running[appID]
		if !running {
			// Its reaper is up, so what its processes say is all they will.
			game = s.start(appID, name, pids, 0)
			game.onReaper = true
			game.verdict = s.judge(appID, name, pids, true)
		}
		if game.verdict == counted {
			windows = append(windows, window(appID, name))
		}
	}
	return windows, nil
}

// Run reports games starting and stopping until ctx is done.
func (s *Source) Run(ctx context.Context, events chan<- source.Event) error {
	emit := func(e source.Event) bool {
		select {
		case events <- e:
			return true
		case <-ctx.Done():
			return false
		}
	}
	readLog := func() (bool, error) {
		lines, err := s.follow.read()
		if err != nil {
			return false, fmt.Errorf("reading Steam's game log: %w", err)
		}
		for _, raw := range lines {
			if !s.handle(parseLine(raw, s.loc), emit) {
				return false, nil
			}
		}
		return true, nil
	}

	if ok, err := readLog(); !ok {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-s.watcher.Events:
			if !ok {
				return nil // closed
			}
			if filepath.Clean(event.Name) != s.logPath || event.Has(fsnotify.Chmod) && !event.Has(fsnotify.Write) {
				continue
			}
			if ok, err := readLog(); !ok {
				return err
			}

		case err, ok := <-s.watcher.Errors:
			if !ok {
				return nil
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				// Some events were dropped; reading catches up regardless.
				if ok, err := readLog(); !ok {
					return err
				}
				continue
			}
			return fmt.Errorf("watching Steam's game log: %w", err)

		case notice := <-s.exits:
			game, ok := s.running[notice.appID]
			if !ok || game.gen != notice.gen {
				continue // already ended, or an earlier run of the game
			}
			// This is the usual way a game ends, not only the backstop for a
			// Steam crash: the reaper's exit gets here a moment before Steam
			// writes its "Remove" line, which then finds the game ended.
			s.stop(notice.appID)
			s.debugf("%s's processes have exited", game.name)
			if !s.ended(notice.appID, game, "", emit) {
				return nil
			}

		case notice := <-s.settles:
			game, ok := s.running[notice.appID]
			if !ok || game.gen != notice.gen || game.verdict != undecided {
				continue // already decided, or an earlier run of the game
			}
			game.settle = nil
			if !s.decide(notice.appID, game, true, emit) {
				return nil
			}
		}
	}
}

// handle acts on one line of the log, and reports false when ctx ended.
func (s *Source) handle(line logLine, emit func(source.Event) bool) bool {
	switch line.kind {
	case lineAdding:
		game, running := s.running[line.appID]
		if !running {
			name, installed := s.library.Name(line.appID)
			if !installed {
				s.debugf("Ignoring Steam app %d: not a Steam game installed here (a shortcut, Remote Play or a tool)", line.appID)
				return true
			}
			live := gameProcesses(s.procDir)[line.appID]
			pids := slices.Clone(live)
			if !slices.Contains(pids, line.pid) {
				pids = append(pids, line.pid)
			}
			game = s.start(line.appID, name, pids, line.pid)
			game.onReaper = len(live) > 0
		}
		// Another process of a game already running: nothing new, unless
		// it's what an undecided game was waiting for, or the game's reaper
		// has come up since and can be watched now.
		switch {
		case game.verdict == undecided:
			if !slices.Contains(game.pids, line.pid) {
				game.pids = append(game.pids, line.pid)
			}
			return s.decide(line.appID, game, false, emit)
		case !game.onReaper:
			s.watchReaper(line.appID, game, gameProcesses(s.procDir)[line.appID])
		}
		return true

	case lineRemoved:
		game, running := s.running[line.appID]
		if !running {
			return true
		}
		s.stop(line.appID)
		return s.ended(line.appID, game, "", emit)

	case lineRestart:
		// The Steam client restarted, and games it was tracking may have
		// ended without a line saying so. Watched games end when their
		// processes do; any that can't be watched are checked now.
		live := gameProcesses(s.procDir)
		for appID, game := range s.running {
			if game.watch == nil && len(live[appID]) == 0 {
				s.stop(appID)
				if !s.ended(appID, game, "%s ended while Steam restarted; ending its session", emit) {
					return false
				}
			}
		}
	}
	return true
}

// decide settles whether an undecided game counts, once its launch processes
// say whose it is, or with what's there when final is set. It reports the
// game opening if it counts, and returns false when ctx ended.
func (s *Source) decide(appID uint64, game *runningGame, final bool, emit func(source.Event) bool) bool {
	live := gameProcesses(s.procDir)[appID]
	pids := slices.Clone(live)
	for _, pid := range game.pids {
		if !slices.Contains(pids, pid) {
			pids = append(pids, pid)
		}
	}
	// Once the reaper is up, the game's processes say all they ever will.
	game.verdict = s.judge(appID, game.name, pids, final || len(live) > 0)
	if game.verdict == undecided {
		if game.settle == nil {
			// Later lines of the launch normally bring this back within a
			// second. If none does, look once more, and no more than that.
			notice := exitNotice{appID: appID, gen: game.gen}
			game.settle = time.AfterFunc(s.settleAfter, func() {
				select {
				case s.settles <- notice:
				case <-s.stopped:
				}
			})
		}
		return true
	}
	if game.settle != nil {
		game.settle.Stop()
		game.settle = nil
	}
	game.pids = nil
	s.watchReaper(appID, game, live)
	if game.verdict == counted {
		return emit(source.Event{Kind: source.Opened, Window: window(appID, game.name), At: game.since})
	}
	return true
}

// judge says whether a game is one to record: with OnlyAccount, one that
// account launched. Whose game it is comes from the environment of its launch
// processes, where Steam puts the account's login name.
//
// Steam logs a game's first process the moment it forks it, before it has
// become the launch wrapper: at that instant nothing says whose game it is,
// so judge answers undecided until a process of the launch (see inLaunch)
// names the account. If the game's reaper is up and still none does (final),
// Steam's connection log gives a second opinion. When neither tells, the
// game doesn't count, so another account's play is never recorded as yours.
func (s *Source) judge(appID uint64, name string, pids []int, final bool) verdict {
	if s.account == 0 {
		return counted
	}
	checked := 0
	for _, pid := range pids {
		if !inLaunch(s.procDir, pid, appID) {
			continue
		}
		checked++
		if login, ok := launchUser(s.procDir, pid); ok {
			return s.judgeLogin(name, login)
		}
	}
	if !final {
		return undecided
	}

	id, err := signedIn(s.root)
	switch {
	case err != nil:
		s.logf("Warning: not recording %s: can't tell which Steam account is playing it: no SteamAppUser in the environment of its %s, and %v",
			name, plural(checked, "launch process", "launch processes"), err)
		return skipped
	case id == s.account:
		s.logf("%s has no SteamAppUser in the environment of its %s; recording it because Steam's connection log says account %d is signed in",
			name, plural(checked, "launch process", "launch processes"), id)
		return counted
	}
	s.logf("Not recording %s: Steam's connection log says another Steam account is signed in, %s", name, s.describe(id))
	return skipped
}

// judgeLogin decides a game from the login name of the account playing it.
func (s *Source) judgeLogin(name, login string) verdict {
	accounts, err := Accounts(s.root)
	if err != nil {
		s.logf("Warning: not recording %s: %v", name, err)
		return skipped
	}
	for _, a := range accounts {
		if strings.EqualFold(a.Login, login) {
			if a.ID == s.account {
				return counted
			}
			s.logf("Not recording %s: it's being played on another Steam account, %s", name, a)
			return skipped
		}
	}
	s.logf("Warning: not recording %s: the Steam account playing it isn't in Steam's list of accounts", name)
	return skipped
}

// describe names an account for the log: with its profile name if Steam's
// list of accounts has it, never with its login name.
func (s *Source) describe(id uint32) string {
	accounts, _ := Accounts(s.root)
	for _, a := range accounts {
		if a.ID == id {
			return a.String()
		}
	}
	return Account{ID: id}.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// start records a game as running, undecided, and watches its processes.
// logged is the PID from the log line that started it, or 0.
func (s *Source) start(appID uint64, name string, pids []int, logged int) *runningGame {
	s.gen++
	game := &runningGame{name: name, since: time.Now(), gen: s.gen}
	game.watch = s.watch(appID, game.gen, pids, logged)
	s.running[appID] = game
	return game
}

// watch has s.exits told when every one of a game's processes has exited.
func (s *Source) watch(appID uint64, gen int, pids []int, logged int) *exitWatch {
	notice := exitNotice{appID: appID, gen: gen}
	still := func(pid int) bool {
		// The PID from the log was just written; any other must still be a
		// launch of this game, not a process that reused its PID.
		id, ok := launchAppID(processArgs(s.procDir, pid))
		return pid == logged || ok && id == appID
	}
	watch, err := watchExits(pids, still, func() {
		select {
		case s.exits <- notice:
		case <-s.stopped:
		}
	})
	if err != nil && !s.pidfdWarned {
		s.logf("Warning: can't watch game processes directly (%v); relying on Steam's log alone", err)
		s.pidfdWarned = true
	}
	return watch
}

// watchReaper moves a game's watch onto its reaper, once that is up: the
// reaper lives exactly as long as the game, which the first process Steam
// logged (the launch wrapper) may not.
func (s *Source) watchReaper(appID uint64, game *runningGame, reapers []int) {
	if game.onReaper || len(reapers) == 0 {
		return
	}
	if game.watch != nil {
		game.watch.stop()
	}
	// A new generation, so a notice the old watch already sent is ignored.
	s.gen++
	game.gen = s.gen
	game.watch = s.watch(appID, game.gen, reapers, 0)
	game.onReaper = true
}

// stop forgets a running game and stops watching it.
func (s *Source) stop(appID uint64) {
	if game, ok := s.running[appID]; ok {
		if game.watch != nil {
			game.watch.stop()
		}
		if game.settle != nil {
			game.settle.Stop()
		}
		delete(s.running, appID)
	}
}

// ended reports the end of a game that was just stopped, if it counted, after
// logging note (about %s, the game) when there is one. It returns false when
// ctx ended.
func (s *Source) ended(appID uint64, game *runningGame, note string, emit func(source.Event) bool) bool {
	switch game.verdict {
	case counted:
		if note != "" {
			s.logf(note, game.name)
		}
		return emit(closed(appID))
	case undecided:
		s.logf("Warning: %s ended before its processes showed which Steam account was playing it, so it wasn't recorded", game.name)
	}
	return true
}

// Close stops watching the log and the games' processes.
func (s *Source) Close() error {
	for appID := range s.running {
		s.stop(appID)
	}
	if !s.stopOnce {
		s.stopOnce = true
		close(s.stopped)
	}
	s.follow.close()
	return s.watcher.Close()
}

func window(appID uint64, name string) source.Window {
	return source.Window{
		ID:    fmt.Sprintf("steam:%d", appID),
		Class: fmt.Sprintf("steam_app_%d", appID),
		Title: name,
		Name:  name,
		Game:  true,
	}
}

func closed(appID uint64) source.Event {
	return source.Event{Kind: source.Closed, Window: source.Window{ID: fmt.Sprintf("steam:%d", appID)}}
}
