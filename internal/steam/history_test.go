package steam

import (
	"strings"
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/models"
)

func at(clock string) time.Time {
	t, err := time.ParseInLocation(logTimeLayout, clock, newYork)
	if err != nil {
		panic(err)
	}
	return t
}

func installed(appID uint64) (string, bool) {
	names := map[uint64]string{1127500: "Mini Motorways", 294100: "RimWorld"}
	name, ok := names[appID]
	return name, ok
}

func lines(text string) []string {
	return strings.Split(strings.ReplaceAll(strings.TrimPrefix(text, "\n"), "\n", "\r\n"), "\n")
}

func TestHistoryPiecesTogetherFinishedSessions(t *testing.T) {
	log := lines(`
[2026-10-01 19:00:00] AppID 1127500 adding PID 100 as a tracked process "/home/deck/.local/share/Steam/ubuntu12_32/reaper SteamLaunch AppId=1127500 -- '/home/deck/.local/share/Steam/steamapps/common/Mini Motorways/Mini Motorways.exe'"
[2026-10-01 19:00:05] AppID 1127500 adding PID 101 as a tracked process "Mini Motorways.exe"
[2026-10-01 20:30:00] AppID 1127500 no longer tracking PID 101, exit code 0
[2026-10-01 20:30:00] AppID 1127500 no longer tracking PID 100, exit code 0
[2026-10-01 20:30:00] Remove 1127500 from running list
[2026-10-02 18:00:00] AppID 620 adding PID 200 as a tracked process "/home/deck/.local/share/Steam/ubuntu12_32/reaper SteamLaunch AppId=620 -- '/home/deck/.local/share/Steam/steamapps/common/SteamLinuxRuntime_sniper'/_v2-entry-point --verb=waitforexitandrun -- '/home/deck/.local/share/Steam/steamapps/common/Proton - Experimental'/proton waitforexitandrun '/home/deck/.local/share/Steam/steamapps/common/Portal 2/portal2.exe'"
[2026-10-02 18:45:00] Remove 620 from running list
[2026-10-03 12:00:00] AppID 13456789012345678901 adding PID 300 as a tracked process "/usr/bin/flatpak run com.discordapp.Discord"
[2026-10-03 13:00:00] AppID 13456789012345678901 no longer tracking PID 300, exit code 0
[2026-10-03 14:00:00] AppID 202355 adding PID 400 as a tracked process "/home/deck/.local/share/Steam/ubuntu12_32/streaming_client"
[2026-10-03 15:00:00] Remove 202355 from running list
` + "\x00\x00\x00\x00" + `[2026-10-04 09:00:00] AppID 294100 adding PID 500 as a tracked process "reaper SteamLaunch AppId=294100"
[2026-10-04 11:00:00] Remove 294100 from running list
[2026-10-05 10:00:00] AppID 294100 adding PID 600 as a tracked process "reaper SteamLaunch AppId=294100"
`)
	got := History(log, newYork, installed)

	want := []PastSession{
		{AppID: 1127500, Name: "Mini Motorways", Start: at("2026-10-01 19:00:00"), End: at("2026-10-01 20:30:00")},
		{AppID: 620, Name: "Portal 2", Start: at("2026-10-02 18:00:00"), End: at("2026-10-02 18:45:00")},
		{AppID: 294100, Name: "RimWorld", Start: at("2026-10-04 09:00:00"), End: at("2026-10-04 11:00:00")},
	}
	if len(got) != len(want) {
		t.Fatalf("History = %+v\nwant %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("session %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestHistorySkipsSessionsWithoutAClearEnd(t *testing.T) {
	log := lines(`
[2026-10-01 19:00:00] AppID 1127500 adding PID 100 as a tracked process
[2026-10-01 19:30:00] Client version: 1762301234
[2026-10-02 08:00:00] AppID 294100 adding PID 200 as a tracked process
[2026-10-02 09:00:00] AppID 294100 no longer tracking PID 200, exit code 0
[2026-10-02 09:05:00] Client version: 1762301234
[2026-10-03 08:00:00] AppID 294100 adding PID 300 as a tracked process
[2026-10-03 08:30:00] AppID 294100 no longer tracking PID 300, exit code 0
[2026-10-03 08:30:20] AppID 294100 adding PID 301 as a tracked process
[2026-10-03 09:30:00] AppID 294100 no longer tracking PID 301, exit code 0
[2026-10-03 12:00:00] AppID 294100 adding PID 302 as a tracked process
[2026-10-03 13:00:00] Remove 294100 from running list
`)
	got := History(log, newYork, installed)

	want := []PastSession{
		// Steam restarted after its processes all stopped: the last stop ends it.
		{AppID: 294100, Name: "RimWorld", Start: at("2026-10-02 08:00:00"), End: at("2026-10-02 09:00:00")},
		// A process handed over within seconds is one session ...
		{AppID: 294100, Name: "RimWorld", Start: at("2026-10-03 08:00:00"), End: at("2026-10-03 09:30:00")},
		// ... and one starting hours later is the next.
		{AppID: 294100, Name: "RimWorld", Start: at("2026-10-03 12:00:00"), End: at("2026-10-03 13:00:00")},
	}
	// Mini Motorways was still running when Steam restarted, so its end is unknown.
	if len(got) != len(want) {
		t.Fatalf("History = %+v\nwant %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("session %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestGameFolderSkipsSteamsTools(t *testing.T) {
	tests := map[string]string{
		"reaper SteamLaunch AppId=1 -- '/x/steamapps/common/SteamLinuxRuntime_sniper'/_v2-entry-point -- '/x/steamapps/common/Proton 9.0'/proton run '/x/steamapps/common/Hades II/Hades2.exe'": "Hades II",
		"reaper SteamLaunch AppId=2 -- '/x/steamapps/common/SteamLinuxRuntime_scout'/run -- /x/steamapps/common/RimWorld/RimWorldLinux":                                                         "RimWorld",
		"reaper SteamLaunch AppId=3 -- '/x/steamapps/common/Proton - Experimental'/proton":                                                                                                      "",
		"streaming_client": "",
	}
	for command, want := range tests {
		if got := gameFolder(command); got != want {
			t.Errorf("gameFolder(%q) = %q, want %q", command, got, want)
		}
	}
}

func TestPlanImport(t *testing.T) {
	past := []PastSession{
		{AppID: 1127500, Name: "Mini Motorways", Start: at("2026-10-01 19:00:00"), End: at("2026-10-01 19:03:00")}, // too short
		{AppID: 1127500, Name: "Mini Motorways", Start: at("2026-10-01 20:00:00"), End: at("2026-10-01 21:00:00")},
		{AppID: 294100, Name: "RimWorld", Start: at("2026-10-02 08:00:00"), End: at("2026-10-02 16:00:00")},
		{AppID: 620, Name: "Portal 2", Start: at("2026-10-03 18:00:00"), End: at("2026-10-03 19:00:00")},
		{AppID: 1145360, Name: "Hades II", Start: at("2026-10-04 18:00:00"), End: at("2026-10-04 19:00:00")},
	}
	existing := []models.SessionLog{
		// Recorded live by gametrak, a second after Steam's line.
		{Game: "Mini Motorways", Class: "steam_app_1127500", Start: "2026-10-01T20:00:01-04:00", End: "2026-10-01T21:00:00-04:00", DurationSeconds: 3599},
		// Logged by hand on the site, under a slightly different spelling.
		{Game: " portal 2 ", Start: "2026-10-03T18:30:00-04:00", DurationSeconds: 1800, Platform: "Steam Deck"},
		// The same game on another day.
		{Game: "Hades II", Class: "steam_app_1145360", Start: "2026-10-05T18:00:00-04:00", End: "2026-10-05T19:00:00-04:00", DurationSeconds: 3600},
	}

	entries, err := PlanImport(past, existing, 5*time.Minute, []int{2}, nil)
	if err != nil {
		t.Fatalf("PlanImport returned error: %v", err)
	}
	type summary struct {
		Number            int
		Name              string
		Recorded, Ignored bool
	}
	want := []summary{
		{1, "Mini Motorways", true, false},
		{2, "RimWorld", false, true},
		{3, "Portal 2", true, false},
		{4, "Hades II", false, false},
	}
	if len(entries) != len(want) {
		t.Fatalf("entries = %+v, want %d", entries, len(want))
	}
	for i, w := range want {
		e := entries[i]
		if got := (summary{e.Number, e.Name, e.Recorded, e.Ignored}); got != w {
			t.Errorf("entry %d = %+v, want %+v", i, got, w)
		}
	}
	if entries[1].Played() <= LongEntry {
		t.Errorf("RimWorld's 8 hours should count as a long entry")
	}

	for _, bad := range [][]int{{0}, {5}, {-1}} {
		if _, err := PlanImport(past, existing, 5*time.Minute, bad, nil); err == nil || !strings.Contains(err.Error(), "numbered 1 to 4") {
			t.Errorf("PlanImport(ignore %v) error = %v, want the valid range", bad, err)
		}
	}

	// An entry ignored in an earlier import stays ignored.
	again, err := PlanImport(past, existing, 5*time.Minute, nil, map[string]bool{past[2].Key(): true})
	if err != nil || !again[1].Ignored || !again[1].IgnoredBefore || again[3].Ignored {
		t.Errorf("entries = %+v, %v; want only RimWorld ignored, from before", again, err)
	}
	if key := past[2].Key(); key != "294100@2026-10-02T12:00:00Z" {
		t.Errorf("Key = %q", key)
	}

	log := entries[3].Log()
	wantLog := models.SessionLog{Game: "Hades II", Class: "steam_app_1145360", Start: "2026-10-04T18:00:00-04:00", End: "2026-10-04T19:00:00-04:00", DurationSeconds: 3600}
	if log != wantLog {
		t.Errorf("Log = %+v, want %+v", log, wantLog)
	}
}
