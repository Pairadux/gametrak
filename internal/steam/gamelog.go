package steam

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// What a line in Steam's game process log says. Lines look like
//
//	[2026-11-10 06:25:31] AppID 1127500 adding PID 18019 as a tracked process "…reaper SteamLaunch AppId=1127500 …"
//	[2026-11-10 06:25:57] AppID 1127500 no longer tracking PID 18019, exit code 0
//	[2026-11-10 06:25:57] Remove 1127500 from running list
//
// and each time the Steam client starts it writes a "Client version: …" line.
type lineKind int

const (
	lineOther   lineKind = iota
	lineAdding           // a process joined an app: the app is running
	lineStopped          // a process of an app exited
	lineRemoved          // the app is no longer running
	lineRestart          // the Steam client started
)

// logLine is one parsed line of the game process log.
type logLine struct {
	kind    lineKind
	at      time.Time // zero when the line has no timestamp
	appID   uint64
	pid     int
	command string // lineAdding: the tracked process's command line, if logged
}

// logTimeLayout is the log's timestamp format: local time, with no offset.
const logTimeLayout = "2006-01-02 15:04:05"

var (
	timestamped = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\]\s*(.*)$`)
	// Only the start of each line is matched, so a Steam update adding
	// something at the end doesn't stop games being seen.
	addingLine  = regexp.MustCompile(`^AppID (\d+) adding PID (\d+) as a tracked process(.*)$`)
	stoppedLine = regexp.MustCompile(`^AppID (\d+) no longer tracking PID (\d+)`)
	removedLine = regexp.MustCompile(`^Remove (\d+) from running list`)
)

// cleanLine removes what the log carries besides text: Windows line endings,
// and NUL bytes, which fill the spots where the file was cut off mid-write
// (a crash or power loss) and run into the next line.
func cleanLine(raw string) string {
	return strings.TrimSpace(strings.ReplaceAll(raw, "\x00", ""))
}

// parseLine reads one line, taking its timestamp to be local time in loc.
func parseLine(raw string, loc *time.Location) logLine {
	text := cleanLine(raw)
	var parsed logLine
	if m := timestamped.FindStringSubmatch(text); m != nil {
		if at, err := time.ParseInLocation(logTimeLayout, m[1], loc); err == nil {
			parsed.at = at
		}
		text = m[2]
	}

	switch {
	case strings.HasPrefix(text, "Client version:"):
		parsed.kind = lineRestart
	case addingLine.MatchString(text):
		m := addingLine.FindStringSubmatch(text)
		parsed.kind, parsed.appID, parsed.pid, parsed.command = lineAdding, number(m[1]), int(number(m[2])), quoted(m[3])
	case stoppedLine.MatchString(text):
		m := stoppedLine.FindStringSubmatch(text)
		parsed.kind, parsed.appID, parsed.pid = lineStopped, number(m[1]), int(number(m[2]))
	case removedLine.MatchString(text):
		m := removedLine.FindStringSubmatch(text)
		parsed.kind, parsed.appID = lineRemoved, number(m[1])
	}
	if parsed.kind != lineOther && parsed.kind != lineRestart && parsed.appID == 0 {
		return logLine{at: parsed.at} // an ID too long to be a number: not a real line
	}
	return parsed
}

// quoted returns the text between the first and last double quote, which is
// how Steam logs a tracked process's command line, or "" if there is none.
func quoted(text string) string {
	first, last := strings.IndexByte(text, '"'), strings.LastIndexByte(text, '"')
	if first < 0 || last <= first {
		return ""
	}
	return text[first+1 : last]
}

// number parses an ID, or returns 0 if it isn't one that fits.
func number(digits string) uint64 {
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// isShortcut reports whether an app ID belongs to a non-Steam shortcut, which
// Steam numbers above the 32-bit range Steam's own apps use.
func isShortcut(appID uint64) bool {
	return appID > 1<<32-1
}
