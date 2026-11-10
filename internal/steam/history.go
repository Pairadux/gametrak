package steam

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/utility"
)

// PastSession is a finished game session pieced together from Steam's game
// log, for importing history from before gametrak ran.
type PastSession struct {
	AppID uint64
	Name  string
	Start time.Time
	End   time.Time
}

// Played is the session's length. Old lines can't show whether the machine
// slept mid-game, so any time asleep is included.
func (p PastSession) Played() time.Duration {
	return p.End.Sub(p.Start)
}

// Key identifies the session across runs, for remembering which were ignored.
func (p PastSession) Key() string {
	return fmt.Sprintf("%d@%s", p.AppID, p.Start.UTC().Format(time.RFC3339))
}

// relaunchGap is how long after an app's last process stopped a new one has
// to start to count as a new session. A launcher handing over to the game
// stops one process and starts another within seconds.
const relaunchGap = time.Minute

// History pieces together the finished sessions of Steam games in the log's
// lines, oldest first, using the same rules as live tracking: a session runs
// from an app's first "adding PID" line to "Remove <id> from running list".
//
//   - Non-Steam shortcuts are left out, and so is anything without a Steam
//     game behind it, like Remote Play streams.
//   - installedName names installed games. A game uninstalled since is named
//     after its folder under steamapps/common, as Steam logged its command.
//   - A session without a clear end is skipped, not guessed: one still
//     running, or one cut off by Steam restarting. If an app's processes
//     all stopped and Steam never removed it, the last stop is the end.
func History(lines []string, loc *time.Location, installedName func(appID uint64) (string, bool)) []PastSession {
	type open struct {
		start    time.Time
		pids     map[int]bool
		lastStop time.Time
		folder   string
	}
	running := map[uint64]*open{}
	var sessions []PastSession

	finish := func(appID uint64, o *open, end time.Time) {
		name, ok := installedName(appID)
		if !ok {
			name = o.folder
		}
		if name == "" || !end.After(o.start) {
			return
		}
		sessions = append(sessions, PastSession{AppID: appID, Name: name, Start: o.start, End: end})
	}

	for _, raw := range lines {
		line := parseLine(raw, loc)
		if line.kind != lineRestart && (line.at.IsZero() || isShortcut(line.appID)) {
			continue
		}
		o := running[line.appID]
		switch line.kind {
		case lineAdding:
			if o != nil && len(o.pids) == 0 && !o.lastStop.IsZero() && line.at.Sub(o.lastStop) > relaunchGap {
				finish(line.appID, o, o.lastStop)
				o = nil
			}
			if o == nil {
				o = &open{start: line.at, pids: map[int]bool{}}
				running[line.appID] = o
			}
			o.pids[line.pid] = true
			if folder := gameFolder(line.command); folder != "" {
				o.folder = folder
			}
		case lineStopped:
			if o != nil {
				delete(o.pids, line.pid)
				if len(o.pids) == 0 {
					o.lastStop = line.at
				}
			}
		case lineRemoved:
			if o != nil {
				finish(line.appID, o, line.at)
				delete(running, line.appID)
			}
		case lineRestart:
			for appID, o := range running {
				if len(o.pids) == 0 && !o.lastStop.IsZero() {
					finish(appID, o, o.lastStop)
				}
			}
			running = map[uint64]*open{}
		}
	}

	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].Start.Before(sessions[j].Start) })
	return sessions
}

var commonFolder = regexp.MustCompile(`/steamapps/common/([^/'"]+)`)

// gameFolder returns the game's folder under steamapps/common in a launch
// command. Proton and the Steam Linux Runtime live there too, ahead of the
// game, so it's the last folder that isn't one of those tools.
func gameFolder(command string) string {
	matches := commonFolder.FindAllStringSubmatch(command, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if name := strings.TrimSpace(matches[i][1]); name != "" && !isTool(name) {
			return name
		}
	}
	return ""
}

func isTool(folder string) bool {
	return folder == "Proton" || strings.HasPrefix(folder, "Proton ") || strings.HasPrefix(folder, "Proton-") ||
		strings.HasPrefix(folder, "SteamLinuxRuntime") || folder == "Steamworks Shared"
}

// Entry is a past session offered for import. Number is what --ignore-entry
// takes. Entries are numbered oldest first over every session long enough to
// keep, so new play only ever adds numbers at the end.
type Entry struct {
	Number int
	PastSession
	// Recorded is set when the session is already in the log, recorded live
	// or imported before; Ignored when --ignore-entry left it out, this time
	// or (IgnoredBefore) in an earlier import.
	Recorded      bool
	Ignored       bool
	IgnoredBefore bool
}

// LongEntry is when an entry gets flagged in the preview: past sessions
// include any time asleep, so very long ones deserve a look.
const LongEntry = 6 * time.Hour

// PlanImport numbers the past sessions at least minimum long and marks those
// already in existing (a session of the same game overlapping it), those in
// ignore, and those whose Key is in ignoredBefore. Every number in ignore must
// be one of the entries.
func PlanImport(past []PastSession, existing []models.SessionLog, minimum time.Duration, ignore []int, ignoredBefore map[string]bool) ([]Entry, error) {
	var entries []Entry
	for _, p := range past {
		if p.Played() >= minimum && p.Played() > 0 {
			before := ignoredBefore[p.Key()]
			entries = append(entries, Entry{Number: len(entries) + 1, PastSession: p, Ignored: before, IgnoredBefore: before})
		}
	}
	for _, n := range ignore {
		if n < 1 || n > len(entries) {
			return nil, fmt.Errorf("--ignore-entry %d: the entries are numbered 1 to %d (see --dry-run)", n, len(entries))
		}
		entries[n-1].Ignored = true
	}
	for i := range entries {
		entries[i].Recorded = Recorded(entries[i].PastSession, existing)
	}
	return entries, nil
}

// Recorded reports whether a session of the same game, by app ID or by name,
// overlaps the past session in the log.
func Recorded(p PastSession, existing []models.SessionLog) bool {
	class := fmt.Sprintf("steam_app_%d", p.AppID)
	name := sameName(p.Name)
	for _, s := range existing {
		if s.Class != class && sameName(s.Game) != name {
			continue
		}
		start, err := s.StartTime()
		if err != nil {
			continue
		}
		end, err := s.EndTime()
		if err != nil || !end.After(start) {
			end = start.Add(s.Duration())
		}
		if start.Before(p.End) && p.Start.Before(end) {
			return true
		}
	}
	return false
}

func sameName(name string) string {
	return strings.ToLower(utility.SanitizeTitle(name))
}

// Log returns the entry as a session for the log.
func (e Entry) Log() models.SessionLog {
	return models.SessionLog{
		Game:            utility.SanitizeTitle(e.Name),
		Class:           fmt.Sprintf("steam_app_%d", e.AppID),
		Start:           e.Start.Format(time.RFC3339),
		End:             e.End.Format(time.RFC3339),
		DurationSeconds: int64(e.Played().Seconds()),
	}
}
