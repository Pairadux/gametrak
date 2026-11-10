package steam

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/austincgause/gametrak/internal/source"
)

// The two accounts in the test loginusers.vdf: the one to record, and
// someone else's on the same machine.
const (
	mine   = 111111111 // SteamID64 76561198071376839
	theirs = 222222222 // SteamID64 76561198182487950
)

// loginUsers is loginusers.vdf as the Steam client writes it.
const loginUsers = `"users"
{
	"76561198071376839"
	{
		"AccountName"		"me_login"
		"PersonaName"		"Me"
		"RememberPassword"		"1"
		"WantsOfflineMode"		"0"
		"SkipOfflineModeWarning"		"0"
		"AllowAutoLogin"		"1"
		"MostRecent"		"0"
		"Timestamp"		"1762770000"
	}
	"76561198182487950"
	{
		"AccountName"		"Them_Login"
		"PersonaName"		"Them"
		"RememberPassword"		"1"
		"WantsOfflineMode"		"0"
		"SkipOfflineModeWarning"		"0"
		"AllowAutoLogin"		"1"
		"MostRecent"		"1"
		"Timestamp"		"1762770900"
	}
}
`

func TestParseAccount(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want uint32
	}{
		{"111111111", mine},
		{" 111111111 ", mine},
		{"[U:1:111111111]", mine},
		{"U:1:111111111", mine},
		{"76561198071376839", mine},
		{"4294967295", 1<<32 - 1},
	} {
		if got, err := ParseAccount(tc.in); err != nil || got != tc.want {
			t.Errorf("ParseAccount(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "0", "me_login", "-5", "[U:1:]", "4294967296", "76561197960265728", "99999999999999999999"} {
		if got, err := ParseAccount(bad); err == nil {
			t.Errorf("ParseAccount(%q) = %d, want an error", bad, got)
		}
	}
}

func TestAccountsReadsLoginUsers(t *testing.T) {
	root, _ := fakeSteam(t)
	if _, err := Accounts(root); err == nil || !strings.Contains(err.Error(), "doesn't exist") {
		t.Errorf("Accounts without loginusers.vdf: error = %v, want it missing", err)
	}

	writeFile(t, LoginUsersPath(root), loginUsers)
	accounts, err := Accounts(root)
	want := []Account{{ID: mine, Login: "me_login", Persona: "Me"}, {ID: theirs, Login: "Them_Login", Persona: "Them"}}
	if err != nil || fmt.Sprint(accounts) != fmt.Sprint(want) || accounts[0].Login != "me_login" {
		t.Errorf("Accounts = %+v, %v; want %+v", accounts, err, want)
	}
	// The daemon's log names accounts without their login names.
	if got := accounts[1].String(); got != "222222222 (Them)" {
		t.Errorf("String = %q", got)
	}

	writeFile(t, LoginUsersPath(root), `"users" {`)
	if _, err := Accounts(root); err == nil {
		t.Error("Accounts of a broken file: want an error")
	}
}

func TestLaunchUser(t *testing.T) {
	proc := t.TempDir()
	environ := func(pid int, vars ...string) {
		writeFile(t, filepath.Join(proc, fmt.Sprint(pid), "environ"), strings.Join(vars, "\x00")+"\x00")
	}
	environ(1, "HOME=/home/deck", "SteamAppUser=me_login", "SteamUser=other", "SteamAppId=1127500")
	environ(2, "SteamUser=me_login")
	environ(3, "HOME=/home/deck", "SteamAppUser=", "SteamAppId=1127500")
	// The Deck's reaper: SteamAppUser set, SteamUser empty.
	environ(5, "SteamAppId=1127500", "SteamAppUser=me_login", "SteamUser=")
	// The process between the launch wrapper and the reaper: neither.
	environ(6, "STEAMSCRIPT=/usr/bin/steam", "SteamUser=")

	for _, tc := range []struct {
		pid  int
		want string
		ok   bool
	}{{1, "me_login", true}, {2, "me_login", true}, {3, "", false}, {4, "", false}, {5, "me_login", true}, {6, "", false}} {
		if got, ok := launchUser(proc, tc.pid); got != tc.want || ok != tc.ok {
			t.Errorf("launchUser(%d) = %q, %v; want %q, %v", tc.pid, got, ok, tc.want, tc.ok)
		}
	}
}

// fakeGame puts a stand-in for a game's reaper in a fake process list,
// launched by login ("" for no account variables). Its environment is like
// the reaper's on the Deck: SteamAppUser set, SteamUser empty.
func fakeGame(t *testing.T, proc string, pid int, appID uint64, login string) {
	t.Helper()
	writeFile(t, filepath.Join(proc, fmt.Sprint(pid), "cmdline"), fmt.Sprintf("reaper\x00SteamLaunch\x00AppId=%d\x00--\x00game\x00", appID))
	env := "HOME=/home/deck\x00SteamAppId=" + fmt.Sprint(appID) + "\x00SteamUser=\x00"
	if login != "" {
		env += "SteamAppUser=" + login + "\x00"
	}
	writeFile(t, filepath.Join(proc, fmt.Sprint(pid), "environ"), env)
}

func (ts *testSource) log() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return strings.Join(ts.logged, "\n")
}

func TestOnlyTheChosenAccountsGamesCount(t *testing.T) {
	root, _ := fakeSteam(t)
	writeFile(t, LoginUsersPath(root), loginUsers)
	proc := t.TempDir()
	fakeGame(t, proc, fakePID, 1127500, "ME_LOGIN") // login names match ignoring case
	fakeGame(t, proc, fakePID+1, 294100, "Them_Login")

	ts := openTestSource(t, root, proc)
	ts.OnlyAccount(mine)
	windows, err := ts.Snapshot()
	if err != nil || len(windows) != 1 || windows[0] != motorways {
		t.Fatalf("Snapshot = %+v, %v; want only my Mini Motorways", windows, err)
	}
	if log := ts.log(); !strings.Contains(log, "Not recording RimWorld: it's being played on another Steam account, 222222222 (Them)") {
		t.Errorf("logged %q, want a note that RimWorld is someone else's", log)
	}
	if strings.Contains(ts.log(), "me_login") || strings.Contains(strings.ToLower(ts.log()), "them_login") {
		t.Errorf("logged %q, which gives away a login name", ts.log())
	}
	ts.run(t)

	// Their game's later lines change nothing, its end included.
	ts.write(t, "[2026-11-10 08:00:00] AppID 294100 adding PID 21000 as a tracked process")
	ts.write(t, "[2026-11-10 08:30:00] Remove 294100 from running list")
	ts.quiet(t)

	// Then I start the same game: that one counts.
	fakeGame(t, proc, fakePID+1, 294100, "me_login")
	ts.write(t, fmt.Sprintf("[2026-11-10 09:00:00] AppID 294100 adding PID %d as a tracked process", fakePID+1))
	if got := ts.next(t); got.Kind != source.Opened || got.Window.Name != "RimWorld" {
		t.Errorf("event = %+v, want my RimWorld opened", got)
	}
	ts.write(t, motorwaysRemoved)
	if got := ts.next(t); got.Kind != source.Closed || got.Window.ID != "steam:1127500" {
		t.Errorf("event = %+v, want Mini Motorways closed", got)
	}
}

func TestGamesWhoseAccountIsUnknownDontCount(t *testing.T) {
	root, _ := fakeSteam(t)
	proc := t.TempDir()
	ts := openTestSource(t, root, proc)
	ts.OnlyAccount(mine)
	ts.run(t)

	// No loginusers.vdf yet: the login can't be matched to an account.
	fakeGame(t, proc, fakePID, 1127500, "me_login")
	ts.write(t, fmt.Sprintf("[2026-11-10 06:25:31] AppID 1127500 adding PID %d as a tracked process", fakePID))
	ts.quiet(t)
	if log := ts.log(); !strings.Contains(log, "Warning: not recording Mini Motorways: Steam's list of accounts") {
		t.Errorf("logged %q, want a warning that the accounts can't be read", log)
	}
	ts.write(t, motorwaysRemoved)
	ts.quiet(t)

	writeFile(t, LoginUsersPath(root), loginUsers)
	for i, login := range []string{"", "somebody_else"} {
		fakeGame(t, proc, fakePID, 294100, login)
		ts.write(t, fmt.Sprintf("[2026-11-10 07:0%d:00] AppID 294100 adding PID %d as a tracked process", i, fakePID))
		ts.write(t, fmt.Sprintf("[2026-11-10 07:0%d:30] Remove 294100 from running list", i))
		ts.quiet(t)
	}
	log := ts.log()
	for _, want := range []string{"can't tell which Steam account is playing it", "the Steam account playing it isn't in Steam's list of accounts"} {
		if !strings.Contains(log, "Warning: not recording RimWorld: "+want) {
			t.Errorf("logged %q, want a warning that %s", log, want)
		}
	}
}

func TestEveryAccountCountsWithoutOnlyAccount(t *testing.T) {
	root, _ := fakeSteam(t)
	writeFile(t, LoginUsersPath(root), loginUsers)
	proc := t.TempDir()
	fakeGame(t, proc, fakePID, 294100, "Them_Login")
	fakeGame(t, proc, fakePID+1, 1127500, "")
	ts := openTestSource(t, root, proc)
	if windows, _ := ts.Snapshot(); len(windows) != 2 {
		t.Errorf("Snapshot = %+v, want both games", windows)
	}
}

// startReaperAs is startReaper for a game that login launched.
func startReaperAs(t *testing.T, appID uint64, login string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sh", "-c", "sleep 30; :", "reaper", "SteamLaunch", fmt.Sprintf("AppId=%d", appID), "--", "game")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "SteamAppUser=" + login, "SteamUser=" + login}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start a stand-in reaper: %v", err)
	}
	t.Cleanup(func() { syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); cmd.Wait() })
	return cmd
}

func TestAnotherAccountsGameEndsQuietly(t *testing.T) {
	root, _ := fakeSteam(t)
	writeFile(t, LoginUsersPath(root), loginUsers)
	ts := openTestSource(t, root, "/proc")
	ts.OnlyAccount(mine)
	ts.run(t)

	// Their game, in real processes, exits without Steam logging it.
	theirGame := startReaperAs(t, 1127500, "Them_Login")
	ts.write(t, fmt.Sprintf("[2026-11-10 06:25:31] AppID 1127500 adding PID %d as a tracked process", theirGame.Process.Pid))
	ts.quiet(t)
	kill(theirGame)
	ts.quiet(t)
	if log := ts.log(); strings.Contains(log, "exited without Steam logging it") {
		t.Errorf("logged %q about someone else's game ending", log)
	}

	// Then mine starts, and counts.
	myGame := startReaperAs(t, 1127500, "me_login")
	ts.write(t, fmt.Sprintf("[2026-11-10 08:00:00] AppID 1127500 adding PID %d as a tracked process", myGame.Process.Pid))
	if got := ts.next(t); got != (source.Event{Kind: source.Opened, Window: motorways}) {
		t.Fatalf("event = %+v, want my Mini Motorways opened", got)
	}
	kill(myGame)
	if got := ts.next(t); got.Kind != source.Closed || got.Window.ID != "steam:1127500" {
		t.Errorf("event = %+v, want my Mini Motorways closed", got)
	}
}

func TestASteamRestartForgetsAnotherAccountsGameQuietly(t *testing.T) {
	root, _ := fakeSteam(t)
	writeFile(t, LoginUsersPath(root), loginUsers)
	proc := t.TempDir()
	ts := openTestSource(t, root, proc)
	ts.OnlyAccount(mine)
	ts.run(t)

	// Their game's processes can't be watched (the PID isn't real), and are
	// gone by the time Steam restarts.
	fakeGame(t, proc, fakePID, 294100, "Them_Login")
	ts.write(t, fmt.Sprintf("[2026-11-10 07:00:00] AppID 294100 adding PID %d as a tracked process", fakePID))
	ts.quiet(t)
	writeFile(t, filepath.Join(proc, fmt.Sprint(fakePID), "cmdline"), "sh\x00")
	ts.write(t, "", clientStarted)
	ts.quiet(t)
	if log := ts.log(); strings.Contains(log, "ended while Steam restarted") {
		t.Errorf("logged %q about someone else's game ending", log)
	}

	// It's forgotten, so my run of the same game counts.
	fakeGame(t, proc, fakePID+1, 294100, "me_login")
	ts.write(t, fmt.Sprintf("[2026-11-10 08:00:00] AppID 294100 adding PID %d as a tracked process", fakePID+1))
	if got := ts.next(t); got.Kind != source.Opened || got.Window.Name != "RimWorld" {
		t.Errorf("event = %+v, want my RimWorld opened", got)
	}
}

func TestTheAccountComesFromWhicheverProcessHasIt(t *testing.T) {
	root, _ := fakeSteam(t)
	writeFile(t, LoginUsersPath(root), loginUsers)
	proc := t.TempDir()
	// On the Deck, a process between the launch wrapper and the reaper has a
	// launch command line but no SteamAppUser; it comes first here.
	writeFile(t, filepath.Join(proc, fmt.Sprint(fakePID), "cmdline"), "reaper\x00SteamLaunch\x00AppId=1127500\x00--\x00game\x00")
	writeFile(t, filepath.Join(proc, fmt.Sprint(fakePID), "environ"), "STEAMSCRIPT=/usr/bin/steam\x00SteamUser=\x00")
	fakeGame(t, proc, fakePID+1, 1127500, "me_login")

	ts := openTestSource(t, root, proc)
	ts.OnlyAccount(mine)
	if windows, _ := ts.Snapshot(); len(windows) != 1 || windows[0] != motorways {
		t.Errorf("Snapshot = %+v, want my Mini Motorways; logged %q", windows, ts.log())
	}
}
