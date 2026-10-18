package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/models"
)

func TestLogAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "sessions.jsonl")
	start := time.Date(2026, 9, 16, 20, 0, 0, 0, time.Local)

	first := models.Session{Class: "RimWorldLinux", GameName: "RimWorld", StartTime: start}
	second := models.Session{Class: "deadlock.exe", GameName: "Deadlock", StartTime: start.Add(3 * time.Hour)}

	if err := Log(path, first, start.Add(90*time.Minute)); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}
	if err := Log(path, second, start.Add(4*time.Hour)); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	sessions, err := LoadAll(path)
	if err != nil {
		t.Fatalf("LoadAll returned error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("loaded %d sessions, want 2", len(sessions))
	}

	if sessions[0].Game != "RimWorld" || sessions[0].Class != "RimWorldLinux" {
		t.Errorf("first session = %+v, want the RimWorld entry", sessions[0])
	}
	if want := int64((90 * time.Minute).Seconds()); sessions[0].DurationSeconds != want {
		t.Errorf("duration = %d, want %d", sessions[0].DurationSeconds, want)
	}
	if got, err := sessions[0].StartTime(); err != nil || !got.Equal(start) {
		t.Errorf("start = %v (err %v), want %v", got, err, start)
	}
}

func TestLoadAllMissingFile(t *testing.T) {
	sessions, err := LoadAll(filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil {
		t.Fatalf("LoadAll on a missing file returned error: %v", err)
	}
	if sessions != nil {
		t.Errorf("loaded %v, want nil", sessions)
	}
}

func TestLoadAllSkipsMalformedLinesAndSanitizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	contents := `{"game":"RimWorld","start":"2026-09-16T20:00:00-04:00","duration_seconds":60}
not json at all

{"game":"ARC\u200bRaiders","start":"2026-09-16T21:00:00-04:00","duration_seconds":60}
`
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	sessions, err := LoadAll(path)
	if err != nil {
		t.Fatalf("LoadAll returned error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("loaded %d sessions, want 2", len(sessions))
	}
	// Invisible characters recorded before sanitization existed are stripped
	// on read so old and new entries group together.
	if sessions[1].Game != "ARCRaiders" {
		t.Errorf("game = %q, want %q", sessions[1].Game, "ARCRaiders")
	}
}

func TestUpdateRewritesTheLogAndKeepsUnreadableLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	contents := `{"game":"RimWorld","start":"2026-09-16T20:00:00-04:00","duration_seconds":60}
not json at all
{"game":"Deadlock","start":"2026-09-16T21:00:00-04:00","duration_seconds":120}
`
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	err := Update(path, func(sessions []models.SessionLog) ([]models.SessionLog, bool) {
		if len(sessions) != 2 {
			t.Errorf("Update passed %d sessions, want 2", len(sessions))
		}
		sessions[0].Game = "RimWorld (renamed)"
		return append(sessions[:1], models.SessionLog{Game: "Zelda"}), true
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	sessions, err := LoadAll(path)
	if err != nil {
		t.Fatalf("LoadAll returned error: %v", err)
	}
	if len(sessions) != 2 || sessions[0].Game != "RimWorld (renamed)" || sessions[1].Game != "Zelda" {
		t.Errorf("sessions after Update = %+v, want the renamed RimWorld, then Zelda", sessions)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "not json at all\n") {
		t.Errorf("rewritten log dropped the unreadable line:\n%s", data)
	}
	if backup, _ := os.ReadFile(path + ".bak"); string(backup) != contents {
		t.Errorf("backup = %q, want the previous contents", backup)
	}
}

func TestUpdateWithoutChangesWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	contents := `{"game":"RimWorld","start":"2026-09-16T20:00:00-04:00","duration_seconds":60}` + "\n"
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	err := Update(path, func(sessions []models.SessionLog) ([]models.SessionLog, bool) {
		return sessions, false
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf("Update without changes wrote a backup (stat err %v)", err)
	}
}

func TestUpdateCreatesAMissingLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "sessions.jsonl")

	err := Update(path, func(sessions []models.SessionLog) ([]models.SessionLog, bool) {
		return append(sessions, models.SessionLog{Game: "Zelda"}), true
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	sessions, err := LoadAll(path)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("LoadAll = %+v (err %v), want the one added session", sessions, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0644 {
		t.Errorf("log mode = %v (err %v), want 0644", info.Mode().Perm(), err)
	}
}
