package steam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Lines as the Deck's Steam client writes them: Windows line endings, a
// command line in quotes, and timestamps in local time without an offset.
const (
	motorwaysAdding  = "[2026-11-10 06:25:31] AppID 1127500 adding PID 18019 as a tracked process \"/home/deck/.local/share/Steam/ubuntu12_32/reaper SteamLaunch AppId=1127500 -- '/home/deck/.local/share/Steam/steamapps/common/SteamLinuxRuntime_sniper'/_v2-entry-point --verb=waitforexitandrun -- '/home/deck/.local/share/Steam/steamapps/common/Proton - Experimental'/proton waitforexitandrun '/home/deck/.local/share/Steam/steamapps/common/Mini Motorways/Mini Motorways.exe'\"\r"
	motorwaysStopped = "[2026-11-10 06:25:57] AppID 1127500 no longer tracking PID 18019, exit code 0\r"
	motorwaysRemoved = "[2026-11-10 06:25:57] Remove 1127500 from running list\r"
	clientStarted    = "[2026-11-10 06:30:02] Client version: 1762301234\r"
)

var newYork = mustLoad("America/New_York")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// writeFile creates path's directory and writes contents to it.
func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func manifest(appID, name string) string {
	return "\"AppState\"\n{\n\t\"appid\"\t\t\"" + appID + "\"\n\t\"Universe\"\t\t\"1\"\n\t\"name\"\t\t\"" + name +
		"\"\n\t\"StateFlags\"\t\t\"4\"\n\t\"installdir\"\t\t\"" + name + "\"\n}\n"
}

// fakeSteam builds a Steam data folder with Mini Motorways installed on
// internal storage and RimWorld on an SD card library.
func fakeSteam(t *testing.T) (root, sdCard string) {
	t.Helper()
	dir := t.TempDir()
	root = filepath.Join(dir, "Steam")
	sdCard = filepath.Join(dir, "SD1T5")
	writeFile(t, filepath.Join(root, "steamapps", "appmanifest_1127500.acf"), manifest("1127500", "Mini Motorways"))
	writeFile(t, filepath.Join(sdCard, "steamapps", "appmanifest_294100.acf"), manifest("294100", "RimWorld"))
	writeFile(t, filepath.Join(root, "steamapps", "libraryfolders.vdf"), `"libraryfolders"
{
	"0"
	{
		"path"		"`+root+`"
		"label"		""
		"contentid"		"123"
		"apps"
		{
			"1127500"		"1234"
		}
	}
	"1"
	{
		"path"		"`+sdCard+`"
		"label"		"SD1T5"
	}
}
`)
	writeFile(t, filepath.Join(root, "logs", "gameprocess_log.txt"), "[2025-08-26 10:00:00] old history\r\n")
	return root, sdCard
}

func TestParseVDF(t *testing.T) {
	parsed, err := parseVDF(`// a comment
"Root"
{
	"Path"	"C:\\Games\\Steam"   // Windows paths are escaped
	"quote"	"say \"hi\""
	"bare"	value
	"win"	"yes"	[$WIN32]
	"Nested" { "a" "1" }
	"dupe"	"first"
	"dupe"	"second"
}`)
	if err != nil {
		t.Fatalf("parseVDF returned error: %v", err)
	}
	root := parsed.obj("root")
	for key, want := range map[string]string{
		"path": `C:\Games\Steam`, "quote": `say "hi"`, "bare": "value", "win": "yes", "dupe": "first",
	} {
		if got := root.str(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if root.obj("nested").str("a") != "1" {
		t.Errorf("nested = %+v", root.obj("nested"))
	}

	for _, broken := range []string{`"a" {`, `"a" "unterminated`, `}`, `"key"`} {
		if _, err := parseVDF(broken); err == nil {
			t.Errorf("parseVDF(%q) accepted a broken file", broken)
		}
	}
}

func TestLibraryFindsGamesInEveryLibraryFolder(t *testing.T) {
	root, sdCard := fakeSteam(t)
	library := NewLibrary(root)

	for id, want := range map[uint64]string{1127500: "Mini Motorways", 294100: "RimWorld"} {
		if name, ok := library.Name(id); !ok || name != want {
			t.Errorf("Name(%d) = %q, %v; want %q", id, name, ok, want)
		}
	}
	if _, ok := library.Name(202355); ok {
		t.Error("found a name for an app that isn't installed")
	}
	if got := library.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}

	// A game installed after the library was read is found on the next miss.
	library.minReload = 0
	writeFile(t, filepath.Join(sdCard, "steamapps", "appmanifest_1145360.acf"), manifest("1145360", "Hades II"))
	if name, ok := library.Name(1145360); !ok || name != "Hades II" {
		t.Errorf("Name of a newly installed game = %q, %v", name, ok)
	}
}

func TestLibraryReadsTheOldLibraryFoldersFormat(t *testing.T) {
	dir := t.TempDir()
	root, other := filepath.Join(dir, "Steam"), filepath.Join(dir, "Games")
	writeFile(t, filepath.Join(other, "steamapps", "appmanifest_70.acf"), manifest("70", "Half-Life"))
	writeFile(t, filepath.Join(root, "steamapps", "libraryfolders.vdf"),
		"\"LibraryFolders\"\n{\n\t\"TimeNextStatsReport\"\t\t\"1690000000\"\n\t\"1\"\t\t\""+other+"\"\n}\n")

	if name, ok := NewLibrary(root).Name(70); !ok || name != "Half-Life" {
		t.Errorf("Name(70) = %q, %v; want Half-Life", name, ok)
	}
}

func TestFindRootAndClasses(t *testing.T) {
	root, _ := fakeSteam(t)
	link := filepath.Join(t.TempDir(), "steam")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if got, err := FindRoot(link); err != nil || got != root {
		t.Errorf("FindRoot(symlink) = %q, %v; want %q", got, err, root)
	}
	if _, err := FindRoot(t.TempDir()); err == nil {
		t.Error("FindRoot accepted a folder that isn't Steam's")
	}

	if id, ok := AppIDFromClass("steam_app_1086940"); !ok || id != 1086940 {
		t.Errorf("AppIDFromClass = %d, %v", id, ok)
	}
	for _, class := range []string{"steam_app_", "steam_app_x", "RimWorldLinux", "steam_app_0"} {
		if _, ok := AppIDFromClass(class); ok {
			t.Errorf("AppIDFromClass(%q) found an app ID", class)
		}
	}
}

func TestParseLine(t *testing.T) {
	at := time.Date(2026, 11, 10, 6, 25, 31, 0, newYork)
	tests := []struct {
		raw  string
		want logLine
	}{
		{motorwaysAdding, logLine{kind: lineAdding, at: at, appID: 1127500, pid: 18019,
			command: "/home/deck/.local/share/Steam/ubuntu12_32/reaper SteamLaunch AppId=1127500 -- '/home/deck/.local/share/Steam/steamapps/common/SteamLinuxRuntime_sniper'/_v2-entry-point --verb=waitforexitandrun -- '/home/deck/.local/share/Steam/steamapps/common/Proton - Experimental'/proton waitforexitandrun '/home/deck/.local/share/Steam/steamapps/common/Mini Motorways/Mini Motorways.exe'"}},
		{motorwaysStopped, logLine{kind: lineStopped, at: at.Add(26 * time.Second), appID: 1127500, pid: 18019}},
		{motorwaysRemoved, logLine{kind: lineRemoved, at: at.Add(26 * time.Second), appID: 1127500}},
		{clientStarted, logLine{kind: lineRestart, at: time.Date(2026, 11, 10, 6, 30, 2, 0, newYork)}},
		{"Client version: 1762301234", logLine{kind: lineRestart}},
		// NUL bytes left by a cut-off write run straight into the next line.
		{"\x00\x00\x00\x00[2026-11-10 06:25:57] Remove 1127500 from running list\r", logLine{kind: lineRemoved, at: at.Add(26 * time.Second), appID: 1127500}},
		{"\x00\x00\x00\x00\x00\x00", logLine{}},
		{"[2026-11-10 06:25:31] AppID 7 adding PID 9 as a tracked process", logLine{kind: lineAdding, at: at, appID: 7, pid: 9}},
		// Anything Steam might add after the command doesn't matter.
		{"[2026-11-10 06:25:31] AppID 7 adding PID 9 as a tracked process \"reaper SteamLaunch AppId=7\" (parent 1)", logLine{kind: lineAdding, at: at, appID: 7, pid: 9, command: "reaper SteamLaunch AppId=7"}},
		{"[2026-11-10 06:25:31] AppID 7 adding PID 9 as a tracked process, sandboxed", logLine{kind: lineAdding, at: at, appID: 7, pid: 9}},
		{"[2026-11-10 06:30:02] SSGL: UI mode (0->4)\r", logLine{at: time.Date(2026, 11, 10, 6, 30, 2, 0, newYork)}},
		{"", logLine{}},
		{"[2026-11-10 06:25:31] AppID 99999999999999999999999 adding PID 1 as a tracked process", logLine{at: at}},
	}
	for _, tc := range tests {
		if got := parseLine(tc.raw, newYork); got != tc.want {
			t.Errorf("parseLine(%q)\n got %+v\nwant %+v", tc.raw, got, tc.want)
		}
	}
	if !isShortcut(13456789012345678901) || isShortcut(1127500) {
		t.Error("isShortcut can't tell a shortcut's 64-bit ID from a Steam app ID")
	}
}

func TestFollowerReadsOnlyNewCompleteLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gameprocess_log.txt")
	writeFile(t, path, "history\r\n")
	f := newFollower(path)
	defer f.close()

	appendTo := func(text string) {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			t.Fatal(err)
		}
		file.WriteString(text)
		file.Close()
	}
	read := func() []string {
		lines, err := f.read()
		if err != nil {
			t.Fatalf("read returned error: %v", err)
		}
		return lines
	}

	if got := read(); len(got) != 0 {
		t.Errorf("read the history: %q", got)
	}
	appendTo("one\r\ntw")
	if got := read(); strings.Join(got, "|") != "one\r" {
		t.Errorf("read = %q, want only the complete line", got)
	}
	appendTo("o\r\n")
	if got := read(); strings.Join(got, "|") != "two\r" {
		t.Errorf("read = %q, want the line finished", got)
	}

	// Cut short, then written again: everything is new.
	writeFile(t, path, "fresh\r\n")
	if got := read(); strings.Join(got, "|") != "fresh\r" {
		t.Errorf("read after truncation = %q", got)
	}

	// Replaced by a new file (a new inode): everything in it is new.
	replacement := path + ".new"
	writeFile(t, replacement, "replaced one\r\nreplaced two\r\n")
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if got := read(); strings.Join(got, "|") != "replaced one\r|replaced two\r" {
		t.Errorf("read after replacement = %q", got)
	}

	// Deleted, then created again.
	os.Remove(path)
	if got := read(); len(got) != 0 {
		t.Errorf("read of a deleted file = %q", got)
	}
	writeFile(t, path, "back\r\n")
	if got := read(); strings.Join(got, "|") != "back\r" {
		t.Errorf("read after recreation = %q", got)
	}
}

func TestFollowerOfAFileThatDoesNotExistYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gameprocess_log.txt")
	f := newFollower(path)
	defer f.close()
	writeFile(t, path, "first\r\n")
	if lines, err := f.read(); err != nil || strings.Join(lines, "|") != "first\r" {
		t.Errorf("read = %q, %v; want the whole new file", lines, err)
	}
}

func TestLaunchAppIDAndTheProcessList(t *testing.T) {
	tests := []struct {
		args []string
		id   uint64
		ok   bool
	}{
		{[]string{"/home/deck/.local/share/Steam/ubuntu12_32/reaper", "SteamLaunch", "AppId=1127500", "--", "game"}, 1127500, true},
		{[]string{"reaper", "SteamLaunch", "AppId=3191030"}, 3191030, true},
		{[]string{"game", "AppId=1"}, 0, false},                        // no SteamLaunch
		{[]string{"reaper", "SteamLaunch", "--", "AppId=5"}, 0, false}, // the game's own arguments
		{[]string{"reaper", "SteamLaunch", "AppId=abc"}, 0, false},
		{nil, 0, false},
	}
	for _, tc := range tests {
		if id, ok := launchAppID(tc.args); id != tc.id || ok != tc.ok {
			t.Errorf("launchAppID(%q) = %d, %v; want %d, %v", tc.args, id, ok, tc.id, tc.ok)
		}
	}

	proc := t.TempDir()
	writeFile(t, filepath.Join(proc, "100", "cmdline"), "reaper\x00SteamLaunch\x00AppId=1127500\x00--\x00game\x00")
	writeFile(t, filepath.Join(proc, "101", "cmdline"), "Mini Motorways.exe\x00")
	writeFile(t, filepath.Join(proc, "102", "cmdline"), "steam-launch-wrapper\x00--\x00reaper\x00SteamLaunch\x00AppId=1127500\x00")
	writeFile(t, filepath.Join(proc, "self", "cmdline"), "reaper\x00SteamLaunch\x00AppId=9\x00")
	writeFile(t, filepath.Join(proc, "uptime"), "1 2\n")
	games := gameProcesses(proc)
	if len(games) != 1 || len(games[1127500]) != 1 || games[1127500][0] != 100 {
		t.Errorf("gameProcesses = %v, want only PID 100 for 1127500", games)
	}
}
