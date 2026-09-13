package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/models"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "active.json")
	now := time.Date(2026, 9, 16, 20, 0, 0, 0, time.Local)

	sessions := []models.Session{
		{Address: "b2", Class: "deadlock.exe", GameName: "Deadlock", StartTime: now.Add(-time.Hour)},
		{Address: "a1", Class: "RimWorldLinux", GameName: "RimWorld", StartTime: now.Add(-2 * time.Hour)},
	}

	if err := Save(path, sessions, now); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !loaded.UpdatedAt.Equal(now) {
		t.Errorf("updated at = %v, want %v", loaded.UpdatedAt, now)
	}
	if loaded.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", loaded.PID, os.Getpid())
	}
	if len(loaded.Sessions) != 2 {
		t.Fatalf("loaded %d sessions, want 2", len(loaded.Sessions))
	}
	// Sessions are stored oldest first regardless of the order they arrive in.
	if loaded.Sessions[0].Game != "RimWorld" {
		t.Errorf("first session = %q, want RimWorld", loaded.Sessions[0].Game)
	}

	restored := loaded.Sessions[0].Session()
	if restored.Address != "a1" || !restored.StartTime.Equal(now.Add(-2*time.Hour)) {
		t.Errorf("restored session = %+v, want the original RimWorld session", restored)
	}
}

func TestLoadMissingFile(t *testing.T) {
	loaded, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load on a missing file returned error: %v", err)
	}
	if len(loaded.Sessions) != 0 || loaded.PID != 0 {
		t.Errorf("loaded %+v, want an empty state", loaded)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Error("Load accepted a corrupt state file")
	}
}

func TestSaveReplacesPreviousState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active.json")
	now := time.Date(2026, 9, 16, 20, 0, 0, 0, time.Local)

	if err := Save(path, []models.Session{{Address: "a1", GameName: "RimWorld", StartTime: now}}, now); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if err := Save(path, nil, now.Add(time.Minute)); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded.Sessions) != 0 {
		t.Errorf("loaded %d sessions, want none", len(loaded.Sessions))
	}

	// The temp file used for the atomic write must not be left behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("failed to read directory: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d files, want only the state file", len(entries))
	}
}

func TestClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active.json")
	now := time.Now()

	if err := Save(path, nil, now); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if err := Clear(path); err != nil {
		t.Fatalf("Clear returned error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("state file still present after Clear (err %v)", err)
	}
	// Clearing an absent file is not an error.
	if err := Clear(path); err != nil {
		t.Errorf("Clear on a missing file returned error: %v", err)
	}
}

func TestDaemonRunning(t *testing.T) {
	if got := (State{PID: os.Getpid()}).DaemonRunning(); !got {
		t.Error("the current process should report as running")
	}
	if got := (State{PID: 0}).DaemonRunning(); got {
		t.Error("an unset pid should not report as running")
	}
}
