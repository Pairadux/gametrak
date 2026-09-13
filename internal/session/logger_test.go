package session

import (
	"os"
	"path/filepath"
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
