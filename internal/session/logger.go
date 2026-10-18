package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/austincgause/gametrak/internal/lockfile"
	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/utility"
)

// lockPath is the lock that serializes appends and rewrites of the log. It is
// a separate file because a rewrite replaces the log itself.
func lockPath(sessionsFile string) string {
	return sessionsFile + ".lock"
}

// Log appends a completed session to the JSONL log file
func Log(sessionsFile string, session models.Session, endTime time.Time) error {
	if err := os.MkdirAll(filepath.Dir(sessionsFile), 0755); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}

	duration := endTime.Sub(session.StartTime)

	entry := models.SessionLog{
		Game:            session.GameName,
		Class:           session.Class,
		Start:           session.StartTime.Format(time.RFC3339),
		End:             endTime.Format(time.RFC3339),
		DurationSeconds: int64(duration.Seconds()),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	release, err := lockfile.Acquire(lockPath(sessionsFile))
	if err != nil {
		return err
	}
	defer release()

	f, err := os.OpenFile(sessionsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open sessions file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write session: %w", err)
	}

	return nil
}

// LoadAll reads all sessions from the JSONL log file. Game names are sanitized
// on read so entries written before title sanitization existed still group and
// align correctly.
func LoadAll(sessionsFile string) ([]models.SessionLog, error) {
	f, err := os.Open(sessionsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read sessions file: %w", err)
	}
	defer f.Close()

	sessions, _, err := parse(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read sessions file: %w", err)
	}
	return sessions, nil
}

// parse reads JSONL sessions, sanitizing game names, and returns the
// non-empty lines it could not parse separately.
func parse(r io.Reader) (sessions []models.SessionLog, malformed [][]byte, err error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var entry models.SessionLog
		if err := json.Unmarshal(line, &entry); err != nil {
			malformed = append(malformed, bytes.Clone(line))
			continue
		}
		entry.Game = utility.SanitizeTitle(entry.Game)
		sessions = append(sessions, entry)
	}
	return sessions, malformed, scanner.Err()
}

// Update rewrites the log under its lock. change receives every session and
// returns the new list and whether anything changed; nothing is written if
// not. Lines that are not valid sessions are kept, at the end of the file,
// and the previous contents are saved alongside as a .bak file.
func Update(sessionsFile string, change func([]models.SessionLog) ([]models.SessionLog, bool)) error {
	if err := os.MkdirAll(filepath.Dir(sessionsFile), 0755); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}

	release, err := lockfile.Acquire(lockPath(sessionsFile))
	if err != nil {
		return err
	}
	defer release()

	previous, err := os.ReadFile(sessionsFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read sessions file: %w", err)
	}

	sessions, malformed, err := parse(bytes.NewReader(previous))
	if err != nil {
		return fmt.Errorf("failed to read sessions file: %w", err)
	}

	updated, changed := change(sessions)
	if !changed {
		return nil
	}

	var buf bytes.Buffer
	for _, entry := range updated {
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to marshal session: %w", err)
		}
		buf.Write(append(data, '\n'))
	}
	for _, line := range malformed {
		buf.Write(append(line, '\n'))
	}

	if len(previous) > 0 {
		if err := os.WriteFile(sessionsFile+".bak", previous, 0644); err != nil {
			return fmt.Errorf("failed to back up sessions file: %w", err)
		}
	}
	return replace(sessionsFile, buf.Bytes())
}

// replace writes data to a sibling temp file and renames it over path, so a
// crash mid-write cannot leave a truncated log behind.
func replace(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("failed to create temp sessions file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write sessions: %w", err)
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to set sessions file mode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close sessions file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("failed to replace sessions file: %w", err)
	}
	return nil
}
