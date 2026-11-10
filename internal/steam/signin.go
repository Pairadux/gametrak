package steam

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ConnectionLogPath is where the Steam client logs its connection to Steam's
// servers. Nearly every line carries the account the client is signed in to:
//
//	[2026-11-10 15:52:01] Client version: 1792618285
//	[2026-11-10 15:52:01] [Logged Off, 0, 0] [U:1:0] CCMInterface::SetSteamID( [U:1:0] )
//	[2026-11-10 15:52:03] [Logged Off, 0, 0] [U:1:1238557435] CCMInterface::SetSteamID( [U:1:1238557435] )
//	[2026-11-10 15:56:09] [Logged Off, 4, 0] [U:1:1238557435] Log session ended
//
// The account is set about three seconds after the client starts, offline
// too (where the state stays "Logged Off"), and switching accounts restarts
// the client.
func ConnectionLogPath(root string) string {
	return filepath.Join(root, "logs", "connection_log.txt")
}

var (
	accountLine       = regexp.MustCompile(`^\[[^\]]*\] \[[^\]]*\] \[U:1:(\d+)\] (.*)$`)
	clientVersionLine = regexp.MustCompile(`^\[[^\]]*\] Client version:`)
)

// signedIn returns the account the running Steam client is signed in to, from
// its connection log. It's the second opinion for a game whose processes
// don't name their account. The error says why the log doesn't tell.
func signedIn(root string) (uint32, error) {
	path := ConnectionLogPath(root)
	files := append([]string{path}, olderConnectionLogs(path)...)
	for i, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			if i == 0 {
				return 0, fmt.Errorf("Steam's connection log can't be read (%v)", err)
			}
			break
		}
		id, err, decided := lastAccount(strings.Split(string(data), "\n"))
		if decided {
			return id, err
		}
		// Steam starts a new file when one fills up, mid-session: what the
		// new one doesn't say yet, the end of the one before it does.
	}
	return 0, errors.New("Steam's connection log doesn't name an account")
}

// lastAccount reads a connection log from its end: the newest line that
// carries an account names who is signed in, unless the client has shut down
// or restarted since. decided is false when the lines say nothing either way.
func lastAccount(lines []string) (id uint32, err error, decided bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		line := cleanLine(lines[i])
		if clientVersionLine.MatchString(line) {
			return 0, errors.New("Steam has just started and hasn't signed in to an account yet"), true
		}
		m := accountLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, parseErr := strconv.ParseUint(m[1], 10, 32)
		if parseErr != nil || n == 0 {
			continue // [U:1:0] is the client before it knows its account
		}
		if strings.HasPrefix(m[2], "Log session ended") {
			return 0, errors.New("Steam's connection log says its last session has ended"), true
		}
		return uint32(n), nil, true
	}
	return 0, nil, false
}

// olderConnectionLogs lists the connection logs Steam set aside when they
// filled up, newest first.
func olderConnectionLogs(current string) []string {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(current), "connection_log*.txt"))
	type aged struct {
		path string
		mod  int64
	}
	var older []aged
	for _, path := range matches {
		if path == current {
			continue
		}
		if info, err := os.Stat(path); err == nil {
			older = append(older, aged{path, info.ModTime().UnixNano()})
		}
	}
	sort.Slice(older, func(i, j int) bool { return older[i].mod > older[j].mod })
	paths := make([]string, len(older))
	for i, o := range older {
		paths[i] = o.path
	}
	return paths
}
