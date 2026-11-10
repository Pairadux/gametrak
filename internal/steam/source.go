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
//   - each running game's processes are also watched (pidfd), so a game still
//     ends when it exits even if Steam crashes and never logs it;
//   - with OnlyAccount, only games launched by that Steam account count.
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

	running     map[uint64]*runningGame
	gen         int
	exits       chan exitNotice
	stopped     chan struct{}
	stopOnce    bool
	pidfdWarned bool
}

type runningGame struct {
	name  string
	gen   int        // tells this run of the game from an earlier one
	watch *exitWatch // nil when the processes can't be watched
	// skipped is set for a game that doesn't count (another account's). It's
	// followed all the same, so its later lines and its end are recognized.
	skipped bool
}

// exitNotice says every watched process of a run of a game has exited.
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
		follow:  newFollower(logPath),
		procDir: "/proc",
		loc:     time.Local,
		logf:    logf,
		debugf:  debugf,
		running: map[uint64]*runningGame{},
		exits:   make(chan exitNotice, 16),
		stopped: make(chan struct{}),
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
			game = s.start(appID, name, pids, 0, !s.counts(name, pids))
		}
		if !game.skipped {
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
			s.stop(notice.appID)
			if game.skipped {
				continue
			}
			s.logf("%s exited without Steam logging it; ending its session", game.name)
			if !emit(closed(notice.appID)) {
				return nil
			}
		}
	}
}

// handle acts on one line of the log, and reports false when ctx ended.
func (s *Source) handle(line logLine, emit func(source.Event) bool) bool {
	switch line.kind {
	case lineAdding:
		if _, running := s.running[line.appID]; running {
			return true // another process of a game already running
		}
		name, installed := s.library.Name(line.appID)
		if !installed {
			s.debugf("Ignoring Steam app %d: not a Steam game installed here (a shortcut, Remote Play or a tool)", line.appID)
			return true
		}
		pids := gameProcesses(s.procDir)[line.appID]
		if !slices.Contains(pids, line.pid) {
			pids = append(pids, line.pid)
		}
		if game := s.start(line.appID, name, pids, line.pid, !s.counts(name, pids)); game.skipped {
			return true
		}
		return emit(source.Event{Kind: source.Opened, Window: window(line.appID, name)})

	case lineRemoved:
		game, running := s.running[line.appID]
		if !running {
			return true
		}
		s.stop(line.appID)
		if game.skipped {
			return true
		}
		return emit(closed(line.appID))

	case lineRestart:
		// The Steam client restarted, and games it was tracking may have
		// ended without a line saying so. Watched games end when their
		// processes do; any that can't be watched are checked now.
		live := gameProcesses(s.procDir)
		for appID, game := range s.running {
			if game.watch == nil && len(live[appID]) == 0 {
				s.stop(appID)
				if game.skipped {
					continue
				}
				s.logf("%s ended while Steam restarted; ending its session", game.name)
				if !emit(closed(appID)) {
					return false
				}
			}
		}
	}
	return true
}

// counts reports whether a game that just started is one to record: with
// OnlyAccount, one that account launched. When the account can't be told, the
// game doesn't count, so another account's play is never recorded as yours.
func (s *Source) counts(name string, pids []int) bool {
	if s.account == 0 {
		return true
	}
	login := ""
	for _, pid := range pids {
		if user, ok := launchUser(s.procDir, pid); ok {
			login = user
			break
		}
	}
	if login == "" {
		s.logf("Warning: not recording %s: can't tell which Steam account is playing it (no SteamAppUser in its processes' environment)", name)
		return false
	}
	accounts, err := Accounts(s.root)
	if err != nil {
		s.logf("Warning: not recording %s: %v", name, err)
		return false
	}
	for _, a := range accounts {
		if strings.EqualFold(a.Login, login) {
			if a.ID == s.account {
				return true
			}
			s.logf("Not recording %s: it's being played on another Steam account, %s", name, a)
			return false
		}
	}
	s.logf("Warning: not recording %s: the Steam account playing it isn't in Steam's list of accounts", name)
	return false
}

// start records a game as running and watches its processes. logged is the
// PID from the log line that started it, or 0. A skipped game is followed the
// same way, but never reported.
func (s *Source) start(appID uint64, name string, pids []int, logged int, skipped bool) *runningGame {
	s.gen++
	game := &runningGame{name: name, gen: s.gen, skipped: skipped}
	notice := exitNotice{appID: appID, gen: s.gen}
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
	game.watch = watch
	s.running[appID] = game
	return game
}

func (s *Source) stop(appID uint64) {
	if game, ok := s.running[appID]; ok {
		if game.watch != nil {
			game.watch.stop()
		}
		delete(s.running, appID)
	}
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
