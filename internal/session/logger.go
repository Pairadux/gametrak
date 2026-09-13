package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/utility"
)

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

	var sessions []models.SessionLog
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry models.SessionLog
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // Skip malformed lines
		}
		entry.Game = utility.SanitizeTitle(entry.Game)
		sessions = append(sessions, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read sessions file: %w", err)
	}

	return sessions, nil
}
