package steam

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// launchAppID returns the app ID in a Steam launch command line. Steam starts
// every game through its reaper, as "reaper SteamLaunch AppId=<id> -- <game>",
// and the reaper lives until the whole game has exited. Arguments after "--"
// belong to the game, so they never count.
func launchAppID(args []string) (uint64, bool) {
	launch := false
	for _, arg := range args {
		switch {
		case arg == "--":
			return 0, false
		case arg == "SteamLaunch":
			launch = true
		case launch && strings.HasPrefix(arg, "AppId="):
			id, err := strconv.ParseUint(strings.TrimPrefix(arg, "AppId="), 10, 64)
			return id, err == nil && id > 0
		}
	}
	return 0, false
}

// processArgs reads a process's command line from procDir ("/proc").
func processArgs(procDir string, pid int) []string {
	data, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
}

// gameProcesses lists, by app ID, the processes Steam launched games under.
// It reads the process list once; nothing here runs on a timer.
func gameProcesses(procDir string) map[uint64][]int {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return nil
	}
	games := map[uint64][]int{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		if id, ok := launchAppID(processArgs(procDir, pid)); ok {
			games[id] = append(games[id], pid)
		}
	}
	return games
}
