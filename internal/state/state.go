// Package state persists the daemon's in-flight sessions so they survive a
// restart and can be inspected by other commands.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/austincgause/gametrak/internal/models"
)

// Active is a session that was still running when the state was written.
type Active struct {
	Address string    `json:"address"`
	Class   string    `json:"class"`
	Title   string    `json:"title"`
	Game    string    `json:"game"`
	Start   time.Time `json:"start"`
}

// Session converts the record back into an in-memory session.
func (a Active) Session() models.Session {
	return models.Session{
		Address:   a.Address,
		Class:     a.Class,
		Title:     a.Title,
		GameName:  a.Game,
		StartTime: a.Start,
	}
}

// State is the daemon's last known set of active sessions. UpdatedAt is
// refreshed on every write, so it doubles as the best estimate of when an
// abruptly killed daemon was last alive.
type State struct {
	PID       int       `json:"pid"`
	UpdatedAt time.Time `json:"updated_at"`
	Sessions  []Active  `json:"sessions"`
}

// DaemonRunning reports whether the process that wrote the state still exists.
func (s State) DaemonRunning() bool {
	if s.PID <= 0 {
		return false
	}
	proc, err := os.FindProcess(s.PID)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// Load reads the state file. A missing file yields an empty state.
func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("failed to read state file: %w", err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("failed to parse state file: %w", err)
	}
	return s, nil
}

// Save atomically writes the active sessions to the state file.
func Save(path string, sessions []models.Session, now time.Time) error {
	s := State{PID: os.Getpid(), UpdatedAt: now}
	for _, sess := range sessions {
		s.Sessions = append(s.Sessions, Active{
			Address: sess.Address,
			Class:   sess.Class,
			Title:   sess.Title,
			Game:    sess.GameName,
			Start:   sess.StartTime,
		})
	}
	sort.Slice(s.Sessions, func(i, j int) bool { return s.Sessions[i].Start.Before(s.Sessions[j].Start) })

	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Write to a sibling temp file and rename so a crash mid-write cannot
	// leave a truncated state file behind.
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("failed to create temp state file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close state file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("failed to replace state file: %w", err)
	}
	return nil
}

// Clear removes the state file, if present.
func Clear(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove state file: %w", err)
	}
	return nil
}
