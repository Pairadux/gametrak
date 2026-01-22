package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/austincgause/gametrak/internal/models"
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

// LoadAll reads all sessions from the JSONL log file
func LoadAll(sessionsFile string) ([]models.SessionLog, error) {
	data, err := os.ReadFile(sessionsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read sessions file: %w", err)
	}

	var sessions []models.SessionLog
	lines := splitLines(data)

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		var entry models.SessionLog
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // Skip malformed lines
		}
		sessions = append(sessions, entry)
	}

	return sessions, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0

	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}

	if start < len(data) {
		lines = append(lines, data[start:])
	}

	return lines
}
