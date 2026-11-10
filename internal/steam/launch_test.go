package steam

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/source"
)

// A game's launch as the Deck showed it. Steam logs each process the moment
// it forks it, so at that instant the process still looks like the one that
// forked it; a moment later it has become its own program:
//
//	the wrapper     SteamAppUser and SteamUser name the account
//	the supervisor  neither, though its command line carries the launch
//	the reaper      SteamAppUser only
const (
	wrapperPID    = fakePID
	supervisorPID = fakePID + 1
	reaperPID     = fakePID + 2
	childPID      = fakePID + 3
)

// fakeProcess puts a process in a fake process list.
func fakeProcess(t *testing.T, proc string, pid int, args []string, env ...string) {
	t.Helper()
	writeFile(t, filepath.Join(proc, fmt.Sprint(pid), "cmdline"), strings.Join(args, "\x00")+"\x00")
	writeFile(t, filepath.Join(proc, fmt.Sprint(pid), "environ"), strings.Join(env, "\x00")+"\x00")
}

// justForked is a process Steam has only just forked: still a copy of the
// Steam client, command line and environment alike.
func justForked(t *testing.T, proc string, pid int) {
	t.Helper()
	fakeProcess(t, proc, pid, []string{"/home/deck/.local/share/Steam/ubuntu12_32/steam", "-steamdeck"},
		"HOME=/home/deck", "STEAMSCRIPT=/usr/bin/steam", "SteamUser=")
}

func launchArgs(program string, appID uint64) []string {
	args := []string{"/home/deck/.local/share/Steam/ubuntu12_32/" + program}
	if program != "reaper" {
		args = append(args, "--", "/home/deck/.local/share/Steam/ubuntu12_32/reaper")
	}
	return append(args, "SteamLaunch", fmt.Sprintf("AppId=%d", appID), "--", "game")
}

func asWrapper(t *testing.T, proc string, pid int, appID uint64, login string) {
	t.Helper()
	env := []string{"HOME=/home/deck", fmt.Sprintf("SteamAppId=%d", appID)}
	if login != "" {
		env = append(env, "SteamAppUser="+login, "SteamUser="+login)
	}
	fakeProcess(t, proc, pid, launchArgs("steam-launch-wrapper", appID), env...)
}

func asSupervisor(t *testing.T, proc string, pid int, appID uint64) {
	t.Helper()
	fakeProcess(t, proc, pid, launchArgs("steam-runtime-supervisor", appID), "HOME=/home/deck", "STEAMSCRIPT=/usr/bin/steam", "SteamUser=")
}

func adding(appID uint64, pid int) string {
	return fmt.Sprintf("[2026-11-10 16:22:20] AppID %d adding PID %d as a tracked process \"…\"", appID, pid)
}

func removed(appID uint64) string {
	return fmt.Sprintf("[2026-11-10 16:28:27] Remove %d from running list", appID)
}

// connectionLog writes Steam's connection log, in the client's own words.
func connectionLog(t *testing.T, root string, lines ...string) {
	t.Helper()
	writeFile(t, ConnectionLogPath(root), strings.Join(lines, "\r\n")+"\r\n")
}

// signIn is what the client logs from its start to being signed in to an
// account while offline, where it never gets to "Logged On".
func signIn(account uint32) []string {
	return []string{
		"",
		"[2026-11-10 15:52:01] Client version: 1792618285",
		"[2026-11-10 15:52:01] Connectivity test: Starting test, fetching 'http://steamconnecttest.com/204'",
		"[2026-11-10 15:52:01] [Logged Off, 0, 0] [U:1:0] CCMInterface::SetSteamID( [U:1:0] )",
		fmt.Sprintf("[2026-11-10 15:52:03] [Logged Off, 0, 0] [U:1:%d] CCMInterface::SetSteamID( [U:1:%d] )", account, account),
		fmt.Sprintf("[2026-11-10 15:52:04] [Logged Off, 4, 0] [U:1:%d] LogOn() called; not connected yet, scheduling connection. Schedule init returned 1", account),
		"[2026-11-10 15:52:05] CCMInterface::RequestWebAccessToken - can't request new access token [3]",
		"[2026-11-10 15:52:05] CClientJobGetWGAuthToken: Failed to get access token for client",
	}
}

func sessionEnded(account uint32) []string {
	return []string{
		fmt.Sprintf("[2026-11-10 15:51:56] [Logged Off, 4, 0] [U:1:%d] ResetVoiceRecording() (was recording: 0) (last get voice time: 1794343916 seconds ago)", account),
		fmt.Sprintf("[2026-11-10 15:51:56] [Logged Off, 4, 0] [U:1:%d] Log session ended", account),
	}
}

func accountSource(t *testing.T) (ts *testSource, root, proc string) {
	t.Helper()
	root, _ = fakeSteam(t)
	writeFile(t, LoginUsersPath(root), loginUsers)
	proc = t.TempDir()
	ts = openTestSource(t, root, proc)
	ts.OnlyAccount(mine)
	return ts, root, proc
}

// The failure v0.2.0 had on the Deck: the account was read the instant Steam
// logged the game's first process, before that process had become the launch
// wrapper, and the game was written off for good.
func TestAGameIsDecidedOnceItsLaunchShowsTheAccount(t *testing.T) {
	ts, _, proc := accountSource(t)
	ts.run(t)

	justForked(t, proc, wrapperPID)
	ts.write(t, adding(1127500, wrapperPID))
	ts.quiet(t) // nothing says whose game it is yet

	// By the time Steam logs the next process, the first is the wrapper.
	asWrapper(t, proc, wrapperPID, 1127500, "me_login")
	justForked(t, proc, supervisorPID)
	ts.write(t, adding(1127500, supervisorPID))
	if got := ts.next(t); got != (source.Event{Kind: source.Opened, Window: motorways}) {
		t.Fatalf("event = %+v, want Mini Motorways opened", got)
	}
	if log := ts.log(); log != "" {
		t.Errorf("logged %q, want nothing", log)
	}

	// The rest of the launch changes nothing.
	asSupervisor(t, proc, supervisorPID, 1127500)
	fakeGame(t, proc, reaperPID, 1127500, "me_login")
	ts.write(t, adding(1127500, reaperPID), adding(1127500, childPID))
	ts.quiet(t)

	ts.write(t, removed(1127500))
	if got := ts.next(t); got.Kind != source.Closed || got.Window.ID != "steam:1127500" {
		t.Errorf("event = %+v, want Mini Motorways closed", got)
	}
}

func TestAnotherAccountsGameIsSkippedOnceItsLaunchShowsTheAccount(t *testing.T) {
	ts, _, proc := accountSource(t)
	ts.run(t)

	justForked(t, proc, wrapperPID)
	ts.write(t, adding(1127500, wrapperPID))
	ts.quiet(t)
	asWrapper(t, proc, wrapperPID, 1127500, "Them_Login")
	justForked(t, proc, supervisorPID)
	asSupervisor(t, proc, supervisorPID, 1127500)
	fakeGame(t, proc, reaperPID, 1127500, "Them_Login")
	ts.write(t, adding(1127500, supervisorPID), adding(1127500, reaperPID), adding(1127500, childPID), removed(1127500))
	ts.quiet(t)

	if log := ts.log(); log != "Not recording Mini Motorways: it's being played on another Steam account, 222222222 (Them)" {
		t.Errorf("logged %q, want one note that it's someone else's", log)
	}
}

// A process that hasn't become part of the launch says nothing about the
// game, whatever is in the environment it was forked with.
func TestOnlyALaunchProcessNamesTheAccount(t *testing.T) {
	ts, _, proc := accountSource(t)
	ts.run(t)

	fakeProcess(t, proc, wrapperPID, []string{"steam", "-steamdeck"}, "SteamAppUser=me_login", "SteamUser=me_login")
	ts.write(t, adding(1127500, wrapperPID))
	ts.quiet(t)

	asWrapper(t, proc, wrapperPID, 1127500, "Them_Login")
	ts.write(t, adding(1127500, supervisorPID))
	ts.quiet(t)
	if log := ts.log(); !strings.Contains(log, "Not recording Mini Motorways: it's being played on another Steam account") {
		t.Errorf("logged %q, want it skipped as someone else's", log)
	}
}

func TestInLaunch(t *testing.T) {
	proc := t.TempDir()
	fakeProcess(t, proc, 1, launchArgs("reaper", 1127500))
	fakeProcess(t, proc, 2, launchArgs("steam-launch-wrapper", 1127500))
	fakeProcess(t, proc, 3, []string{"sh", "-c", "/x/steam-launch-wrapper -- /x/reaper SteamLaunch AppId=1127500 -- game"})
	fakeProcess(t, proc, 4, []string{"steam", "-steamdeck"})
	fakeProcess(t, proc, 5, launchArgs("reaper", 11275001))
	fakeProcess(t, proc, 6, []string{"sh", "-c", "reaper SteamLaunch AppId=11275001 -- game"})
	fakeProcess(t, proc, 7, []string{"Mini Motorways.exe"})

	for pid, want := range map[int]bool{1: true, 2: true, 3: true, 4: false, 5: false, 6: false, 7: false, 8: false} {
		if got := inLaunch(proc, pid, 1127500); got != want {
			t.Errorf("inLaunch(%d) = %v, want %v", pid, got, want)
		}
	}
}

// If Steam's launch processes carry no account at all, its connection log
// says who is signed in.
func TestTheConnectionLogDecidesWhenNoProcessNamesTheAccount(t *testing.T) {
	launch := func(t *testing.T, ts *testSource, proc string) {
		t.Helper()
		justForked(t, proc, wrapperPID)
		ts.write(t, adding(1127500, wrapperPID))
		ts.quiet(t)
		asWrapper(t, proc, wrapperPID, 1127500, "")
		ts.write(t, adding(1127500, supervisorPID))
		ts.quiet(t) // the reaper isn't up: its environment may yet tell
		asSupervisor(t, proc, supervisorPID, 1127500)
		fakeGame(t, proc, reaperPID, 1127500, "")
		ts.write(t, adding(1127500, reaperPID))
	}

	t.Run("mine", func(t *testing.T) {
		ts, root, proc := accountSource(t)
		connectionLog(t, root, append(sessionEnded(theirs), signIn(mine)...)...)
		ts.run(t)
		launch(t, ts, proc)
		if got := ts.next(t); got != (source.Event{Kind: source.Opened, Window: motorways}) {
			t.Fatalf("event = %+v, want Mini Motorways opened", got)
		}
		want := "Mini Motorways has no SteamAppUser in the environment of its 3 launch processes; recording it because Steam's connection log says account 111111111 is signed in"
		if log := ts.log(); log != want {
			t.Errorf("logged %q, want %q", log, want)
		}
		ts.write(t, removed(1127500))
		if got := ts.next(t); got.Kind != source.Closed {
			t.Errorf("event = %+v, want Mini Motorways closed", got)
		}
	})

	t.Run("someone else's", func(t *testing.T) {
		ts, root, proc := accountSource(t)
		connectionLog(t, root, append(sessionEnded(mine), signIn(theirs)...)...)
		ts.run(t)
		launch(t, ts, proc)
		ts.quiet(t)
		want := "Not recording Mini Motorways: Steam's connection log says another Steam account is signed in, 222222222 (Them)"
		if log := ts.log(); log != want {
			t.Errorf("logged %q, want %q", log, want)
		}
	})

	for name, tc := range map[string]struct {
		lines []string
		want  string
	}{
		"no log":             {nil, "Steam's connection log can't be read"},
		"just started":       {signIn(mine)[:4], "Steam has just started and hasn't signed in to an account yet"},
		"session ended":      {append(signIn(mine), sessionEnded(mine)...), "Steam's connection log says its last session has ended"},
		"nothing in the log": {[]string{"[2026-11-10 15:52:05] CClientJobGetWGAuthToken: Failed to get access token for client"}, "Steam's connection log doesn't name an account"},
	} {
		t.Run(name, func(t *testing.T) {
			ts, root, proc := accountSource(t)
			if tc.lines != nil {
				connectionLog(t, root, tc.lines...)
			}
			ts.run(t)
			launch(t, ts, proc)
			ts.quiet(t)
			log := ts.log()
			if !strings.HasPrefix(log, "Warning: not recording Mini Motorways: can't tell which Steam account is playing it: no SteamAppUser in the environment of its 3 launch processes, and ") ||
				!strings.Contains(log, tc.want) {
				t.Errorf("logged %q, want a warning that %s", log, tc.want)
			}
		})
	}
}

func TestSignedIn(t *testing.T) {
	online := func(account uint32) string {
		return fmt.Sprintf("[2026-11-10 15:57:18] [Logged On, 4, 19] [U:1:%d] RecvMsgClientLogOnResponse() : processing complete", account)
	}
	for name, tc := range map[string]struct {
		lines []string
		want  uint32
	}{
		"signed in offline":         {signIn(mine), mine},
		"logged on":                 {append(signIn(mine), online(mine), "[2026-11-10 15:57:19] CClientJobGetClientUpdateHosts: cached version not expired"), mine},
		"after someone else":        {append(append(signIn(theirs), sessionEnded(theirs)...), signIn(mine)...), mine},
		"someone else after me":     {append(append(signIn(mine), sessionEnded(mine)...), signIn(theirs)...), theirs},
		"NUL bytes ahead of a line": {[]string{"\x00\x00\x00" + online(mine)}, mine},
		"restarted, not signed in":  {append(signIn(mine), signIn(mine)[:4]...), 0},
		"shut down":                 {append(signIn(mine), sessionEnded(mine)...), 0},
		"an ID too big to be one":   {[]string{"[2026-11-10 15:57:18] [Logged On, 4, 19] [U:1:99999999999] x"}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			root, _ := fakeSteam(t)
			connectionLog(t, root, tc.lines...)
			got, err := signedIn(root)
			if got != tc.want || (err == nil) != (tc.want != 0) {
				t.Errorf("signedIn = %d, %v; want %d", got, err, tc.want)
			}
		})
	}

	// A log Steam has just started afresh says nothing yet: the one it set
	// aside does.
	root, _ := fakeSteam(t)
	writeFile(t, filepath.Join(root, "logs", "connection_log.previous.txt"), strings.Join(signIn(mine), "\r\n")+"\r\n")
	connectionLog(t, root, "[2026-11-10 15:57:19] CClientJobGetClientUpdateHosts: cached version not expired")
	if got, err := signedIn(root); got != mine || err != nil {
		t.Errorf("signedIn across a new log file = %d, %v; want %d", got, err, mine)
	}
	// And once the new one names an account, the old one is left alone.
	connectionLog(t, root, sessionEnded(mine)...)
	if got, err := signedIn(root); err == nil {
		t.Errorf("signedIn = %d after the session ended, want an error", got)
	}
	if _, err := signedIn(t.TempDir()); err == nil {
		t.Error("signedIn without a log: want an error")
	}
}

// If the launch never shows whose game it is and no later line brings it up
// again, the game is decided once, a little later, with what's there.
func TestAnUndecidedGameIsSettledAfterAWhile(t *testing.T) {
	ts, root, proc := accountSource(t)
	connectionLog(t, root, signIn(mine)...)
	ts.settleAfter = 400 * time.Millisecond
	ts.run(t)

	justForked(t, proc, wrapperPID)
	before := time.Now()
	ts.write(t, adding(1127500, wrapperPID))
	ts.quiet(t)
	asWrapper(t, proc, wrapperPID, 1127500, "me_login") // no line follows
	if got := ts.next(t); got != (source.Event{Kind: source.Opened, Window: motorways}) {
		t.Fatalf("event = %+v, want Mini Motorways opened", got)
	}
	// The game started when Steam logged it, not when it was decided.
	if ts.at.Before(before) || ts.at.Sub(before) > 250*time.Millisecond {
		t.Errorf("the game started %v after its first line, want it to start with the line", ts.at.Sub(before))
	}
	if log := ts.log(); log != "" {
		t.Errorf("logged %q, want nothing: its wrapper named the account", log)
	}
	ts.write(t, removed(1127500))
	ts.next(t)

	// With nothing in its processes, the connection log decides.
	justForked(t, proc, wrapperPID)
	ts.write(t, adding(294100, wrapperPID))
	if got := ts.next(t); got.Kind != source.Opened || got.Window.Name != "RimWorld" {
		t.Fatalf("event = %+v, want RimWorld opened", got)
	}
	if log := ts.log(); !strings.Contains(log, "RimWorld has no SteamAppUser in the environment of its 0 launch processes; recording it because Steam's connection log says account 111111111 is signed in") {
		t.Errorf("logged %q, want a note that the connection log decided", log)
	}
}

func TestAGameThatEndsUndecidedIsntRecorded(t *testing.T) {
	ts, root, proc := accountSource(t)
	connectionLog(t, root, signIn(mine)...)
	ts.settleAfter = 150 * time.Millisecond
	ts.run(t)

	justForked(t, proc, wrapperPID)
	ts.write(t, adding(1127500, wrapperPID), removed(1127500))
	ts.quiet(t) // longer than settleAfter: the game is gone, so nothing settles it
	want := "Warning: Mini Motorways ended before its processes showed which Steam account was playing it, so it wasn't recorded"
	if log := ts.log(); log != want {
		t.Errorf("logged %q, want %q", log, want)
	}

	// It's forgotten: the next launch is judged afresh.
	asWrapper(t, proc, wrapperPID, 1127500, "me_login")
	ts.write(t, adding(1127500, wrapperPID))
	if got := ts.next(t); got != (source.Event{Kind: source.Opened, Window: motorways}) {
		t.Errorf("event = %+v, want Mini Motorways opened", got)
	}
	ts.quiet(t)
}

// startLaunch starts a real process that becomes part of a game's launch only
// after delay, the way a process Steam has just forked does. program stands
// in for its name, and env for the environment it launches with.
func startLaunch(t *testing.T, delay time.Duration, program string, appID uint64, env ...string) *exec.Cmd {
	t.Helper()
	script := fmt.Sprintf(`sleep %.2f; exec env %s sh -c 'sleep 30; :' %s SteamLaunch AppId=%d -- game`,
		delay.Seconds(), strings.Join(env, " "), program, appID)
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "SteamUser="}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start a stand-in launch process: %v", err)
	}
	t.Cleanup(func() { syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); cmd.Wait() })
	return cmd
}

// launched waits until a process started by startLaunch has become its
// program.
func launched(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if args := processArgs("/proc", cmd.Process.Pid); len(args) > 2 && args[2] == "sleep 30; :" {
			return
		}
	}
	t.Fatal("the stand-in launch process never became its program")
}

// The same launch in real processes, each logged before it has become its
// program, and the wrapper gone long before the game is.
func TestARealLaunchIsDecidedAndFollowedByItsReaper(t *testing.T) {
	root, _ := fakeSteam(t)
	writeFile(t, LoginUsersPath(root), loginUsers)
	ts := openTestSource(t, root, "/proc")
	ts.OnlyAccount(mine)
	ts.run(t)

	wrapper := startLaunch(t, 300*time.Millisecond, "steam-launch-wrapper -- reaper", 1127500, "SteamAppUser=me_login", "SteamUser=me_login")
	ts.write(t, adding(1127500, wrapper.Process.Pid))
	ts.quiet(t) // it isn't the wrapper yet

	launched(t, wrapper)
	reaper := startLaunch(t, 200*time.Millisecond, "reaper", 1127500, "SteamAppUser=me_login", "SteamUser=")
	ts.write(t, adding(1127500, reaper.Process.Pid))
	if got := ts.next(t); got != (source.Event{Kind: source.Opened, Window: motorways}) {
		t.Fatalf("event = %+v, want Mini Motorways opened", got)
	}
	if log := ts.log(); log != "" {
		t.Errorf("logged %q, want nothing", log)
	}

	// Once the reaper is up, the next line moves the watch onto it, so the
	// wrapper going away doesn't end the game.
	launched(t, reaper)
	ts.write(t, adding(1127500, childPID))
	ts.quiet(t)
	kill(wrapper)
	ts.quiet(t)

	// The reaper going away does, when Steam never logs it.
	kill(reaper)
	if got := ts.next(t); got.Kind != source.Closed || got.Window.ID != "steam:1127500" {
		t.Fatalf("event = %+v, want Mini Motorways closed", got)
	}
	if log := ts.log(); log != "Mini Motorways exited without Steam logging it; ending its session" {
		t.Errorf("logged %q", log)
	}
}
