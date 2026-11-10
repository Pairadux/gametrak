package steam

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/source"
)

// fakePID is far above any real PID, so the kernel never has such a process.
const fakePID = 1 << 30

// startReaper starts a real process whose command line looks like Steam's
// reaper for appID, as a stand-in for a running game.
func startReaper(t *testing.T, appID uint64) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sh", "-c", "sleep 30; :", "reaper", "SteamLaunch", fmt.Sprintf("AppId=%d", appID), "--", "game")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start a stand-in reaper: %v", err)
	}
	t.Cleanup(func() { syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); cmd.Wait() })
	return cmd
}

func kill(cmd *exec.Cmd) {
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	cmd.Wait()
}

func TestExitWatchFiresOnceEveryProcessHasExited(t *testing.T) {
	first, second := startReaper(t, 1), startReaper(t, 1)
	fired := make(chan struct{}, 1)
	w, err := watchExits([]int{first.Process.Pid, second.Process.Pid}, nil, func() { fired <- struct{}{} })
	if err != nil || w == nil {
		t.Fatalf("watchExits = %v, %v", w, err)
	}
	defer w.stop()

	kill(first)
	select {
	case <-fired:
		t.Fatal("fired while a process was still running")
	case <-time.After(150 * time.Millisecond):
	}
	kill(second)
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("did not fire after every process exited")
	}
}

func TestExitWatchStopsWithoutFiring(t *testing.T) {
	cmd := startReaper(t, 1)
	fired := make(chan struct{}, 1)
	w, err := watchExits([]int{cmd.Process.Pid}, nil, func() { fired <- struct{}{} })
	if err != nil || w == nil {
		t.Fatalf("watchExits = %v, %v", w, err)
	}
	w.stop()
	w.stop() // twice is fine
	kill(cmd)
	select {
	case <-fired:
		t.Fatal("fired after stop")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestExitWatchSkipsProcessesThatAreGoneOrReused(t *testing.T) {
	if w, err := watchExits([]int{fakePID}, nil, func() {}); w != nil || err != nil {
		t.Errorf("watch of a process that doesn't exist = %v, %v; want nothing to watch", w, err)
	}
	cmd := startReaper(t, 1)
	if w, err := watchExits([]int{cmd.Process.Pid}, func(int) bool { return false }, func() {}); w != nil || err != nil {
		t.Errorf("watch of a reused PID = %v, %v; want nothing to watch", w, err)
	}
}

// testSource opens a Source on root that reads processes from procDir, and
// collects what it logs.
type testSource struct {
	*Source
	root   string
	events chan source.Event
	mu     sync.Mutex
	logged []string
}

func openTestSource(t *testing.T, root, procDir string) *testSource {
	t.Helper()
	ts := &testSource{root: root, events: make(chan source.Event)}
	s, err := Open(root, func(format string, args ...any) {
		ts.mu.Lock()
		defer ts.mu.Unlock()
		ts.logged = append(ts.logged, fmt.Sprintf(format, args...))
	}, nil)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	s.procDir = procDir
	s.loc = newYork
	ts.Source = s
	t.Cleanup(func() { s.Close() })
	return ts
}

func (ts *testSource) run(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ts.Run(ctx, ts.events) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	})
}

func (ts *testSource) write(t *testing.T, lines ...string) {
	t.Helper()
	file, err := os.OpenFile(GameLogPath(ts.root), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, line := range lines {
		file.WriteString(strings.TrimSuffix(line, "\r") + "\r\n")
	}
}

func (ts *testSource) next(t *testing.T) source.Event {
	t.Helper()
	select {
	case e := <-ts.events:
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("no event arrived")
		return source.Event{}
	}
}

func (ts *testSource) quiet(t *testing.T) {
	t.Helper()
	select {
	case e := <-ts.events:
		t.Fatalf("unexpected event %+v", e)
	case <-time.After(300 * time.Millisecond):
	}
}

var motorways = source.Window{ID: "steam:1127500", Class: "steam_app_1127500", Title: "Mini Motorways", Name: "Mini Motorways", Game: true}

func TestSourceFollowsGamesThroughSteamsLog(t *testing.T) {
	root, _ := fakeSteam(t)
	// A game that started and never stopped before gametrak ran is history:
	// what's running now comes from the process list.
	writeFile(t, GameLogPath(root), "[2025-08-26 10:00:00] AppID 294100 adding PID 5 as a tracked process\r\n")
	ts := openTestSource(t, root, t.TempDir())
	if windows, _ := ts.Snapshot(); len(windows) != 0 {
		t.Fatalf("Snapshot = %+v, want nothing running", windows)
	}
	ts.run(t)

	ts.write(t, motorwaysAdding)
	if got := ts.next(t); got != (source.Event{Kind: source.Opened, Window: motorways}) {
		t.Errorf("event = %+v, want Mini Motorways opened", got)
	}
	// Further processes of a running game change nothing.
	ts.write(t, "[2026-11-10 06:25:33] AppID 1127500 adding PID 18030 as a tracked process \"Mini Motorways.exe\"")
	ts.quiet(t)

	ts.write(t, motorwaysStopped, motorwaysRemoved)
	if got := ts.next(t); got != (source.Event{Kind: source.Closed, Window: source.Window{ID: "steam:1127500"}}) {
		t.Errorf("event = %+v, want Mini Motorways closed", got)
	}

	// Remote Play and a non-Steam shortcut aren't installed games: no events.
	ts.write(t,
		"[2026-11-10 07:00:00] AppID 202355 adding PID 20000 as a tracked process \"streaming_client\"",
		"[2026-11-10 07:00:01] AppID 13456789012345678901 adding PID 20001 as a tracked process \"Discord\"",
		"[2026-11-10 07:05:00] Remove 202355 from running list",
	)
	ts.quiet(t)

	// A game on the SD card, with NUL bytes ahead of its line.
	ts.write(t, "\x00\x00\x00[2026-11-10 08:00:00] AppID 294100 adding PID 21000 as a tracked process \"reaper SteamLaunch AppId=294100\"")
	if got := ts.next(t); got.Kind != source.Opened || got.Window.Name != "RimWorld" || got.Window.ID != "steam:294100" {
		t.Errorf("event = %+v, want RimWorld opened", got)
	}
}

func TestSourceReadsLinesWrittenBeforeRunStarts(t *testing.T) {
	root, _ := fakeSteam(t)
	ts := openTestSource(t, root, t.TempDir())
	ts.write(t, motorwaysAdding)
	ts.run(t)
	if got := ts.next(t); got != (source.Event{Kind: source.Opened, Window: motorways}) {
		t.Errorf("event = %+v, want the line written between Open and Run", got)
	}
}

func TestSnapshotFindsGamesAlreadyRunning(t *testing.T) {
	root, _ := fakeSteam(t)
	proc := t.TempDir()
	writeFile(t, filepath.Join(proc, fmt.Sprint(fakePID), "cmdline"), "reaper\x00SteamLaunch\x00AppId=294100\x00--\x00RimWorld\x00")
	writeFile(t, filepath.Join(proc, fmt.Sprint(fakePID+1), "cmdline"), "reaper\x00SteamLaunch\x00AppId=202355\x00")

	ts := openTestSource(t, root, proc)
	windows, err := ts.Snapshot()
	if err != nil || len(windows) != 1 || windows[0].ID != "steam:294100" || windows[0].Name != "RimWorld" {
		t.Fatalf("Snapshot = %+v, %v; want only RimWorld", windows, err)
	}
	ts.run(t)

	// Steam's own line ends it as usual.
	ts.write(t, "[2026-11-10 09:00:00] Remove 294100 from running list")
	if got := ts.next(t); got.Kind != source.Closed || got.Window.ID != "steam:294100" {
		t.Errorf("event = %+v, want RimWorld closed", got)
	}
}

func TestAGameStillEndsWhenSteamNeverLogsIt(t *testing.T) {
	root, _ := fakeSteam(t)
	reaper := startReaper(t, 1127500)
	ts := openTestSource(t, root, "/proc")
	ts.run(t)

	ts.write(t, fmt.Sprintf("[2026-11-10 06:25:31] AppID 1127500 adding PID %d as a tracked process \"reaper SteamLaunch AppId=1127500\"", reaper.Process.Pid))
	if got := ts.next(t); got != (source.Event{Kind: source.Opened, Window: motorways}) {
		t.Fatalf("event = %+v, want Mini Motorways opened", got)
	}
	ts.quiet(t)

	// Steam crashes: the game's processes exit, and nothing is logged.
	kill(reaper)
	if got := ts.next(t); got.Kind != source.Closed || got.Window.ID != "steam:1127500" {
		t.Fatalf("event = %+v, want Mini Motorways closed", got)
	}
	ts.mu.Lock()
	logged := strings.Join(ts.logged, "\n")
	ts.mu.Unlock()
	if !strings.Contains(logged, "exited without Steam logging it") {
		t.Errorf("logged %q, want a note that Steam never logged the exit", logged)
	}

	// A late line from Steam about the same run changes nothing.
	ts.write(t, motorwaysRemoved)
	ts.quiet(t)
}

func TestASteamRestartEndsGamesWhoseProcessesAreGone(t *testing.T) {
	root, _ := fakeSteam(t)
	proc := t.TempDir()
	ts := openTestSource(t, root, proc)
	ts.run(t)

	// Neither game's processes can be watched (their PIDs aren't real), so a
	// restart is when gametrak checks on them.
	ts.write(t, motorwaysAdding, "[2026-11-10 06:26:00] AppID 294100 adding PID 30000 as a tracked process")
	ts.next(t)
	ts.next(t)
	writeFile(t, filepath.Join(proc, fmt.Sprint(fakePID), "cmdline"), "reaper\x00SteamLaunch\x00AppId=294100\x00")

	ts.write(t, "", "", clientStarted, "[2026-11-10 06:30:02] SSGL: UI mode (0->4)")
	if got := ts.next(t); got.Kind != source.Closed || got.Window.ID != "steam:1127500" {
		t.Errorf("event = %+v, want Mini Motorways closed", got)
	}
	ts.quiet(t) // RimWorld's reaper is still there, so it keeps running
}
