package logbook

import (
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/models"
)

var newYork = mustLoad("America/New_York")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func TestMinutes(t *testing.T) {
	for seconds, want := range map[int64]int{0: 1, 20: 1, 89: 1, 90: 2, 5700: 95, 5729: 95, 5730: 96} {
		if got := Minutes(seconds); got != want {
			t.Errorf("Minutes(%d) = %d, want %d", seconds, got, want)
		}
	}
}

func TestMergeKeepsAnUnchangedSessionExactly(t *testing.T) {
	local := []models.SessionLog{{
		Game: "Deadlock", Class: "deadlock.exe", Start: "2026-10-15T20:15:03-04:00", End: "2026-10-15T21:50:30-04:00",
		DurationSeconds: 5727, Platform: "PC", LogbookID: "r1",
	}}
	rows := []Row{{ID: "r1", OccurredAt: "2026-10-16T00:15:03.000Z", Data: RowData{Game: "Deadlock", Minutes: 95, Platform: "PC"}}}

	merged, changes := Merge(local, rows, newYork)
	if changes.Any() {
		t.Errorf("changes = %+v, want none", changes)
	}
	if len(merged) != 1 || merged[0] != local[0] {
		t.Errorf("merged = %+v, want the session untouched", merged)
	}
}

func TestMergeAppliesSiteEditsAndDeletes(t *testing.T) {
	local := []models.SessionLog{
		{Game: "Steam Games", Class: "steam_app_1", Start: "2026-10-15T20:00:00-04:00", End: "2026-10-15T21:00:00-04:00",
			DurationSeconds: 3600, Platform: "PC", LogbookID: "renamed"},
		{Game: "RimWorld", Class: "RimWorldLinux", Start: "2026-10-16T20:00:00-04:00", End: "2026-10-16T21:00:00-04:00",
			DurationSeconds: 3600, Platform: "PC", LogbookID: "gone"},
		{Game: "Deadlock", Class: "deadlock.exe", Start: "2026-10-17T20:00:00-04:00", End: "2026-10-17T20:30:10-04:00",
			DurationSeconds: 1810, Platform: "PC", LogbookID: "resized"},
		{Game: "Half-Life 2", Class: "hl2_linux", Start: "2026-10-18T20:00:00-04:00", End: "2026-10-18T21:00:00-04:00",
			DurationSeconds: 3600},
	}
	rows := []Row{
		{ID: "renamed", OccurredAt: "2026-10-16T00:00:00.000Z", Data: RowData{Game: "Baldur's Gate 3", Minutes: 60, Platform: "PC"}},
		{ID: "resized", OccurredAt: "2026-10-18T00:10:00.000Z", Data: RowData{Game: "Deadlock", Minutes: 45, Platform: "PC"}},
	}

	merged, changes := Merge(local, rows, newYork)
	if changes != (Changes{Updated: 2, Deleted: 1}) {
		t.Errorf("changes = %+v, want 2 updated and 1 deleted", changes)
	}
	if len(merged) != 3 {
		t.Fatalf("merged %d sessions, want 3: %+v", len(merged), merged)
	}

	renamed := merged[0]
	if renamed.Game != "Baldur's Gate 3" || renamed.Class != "steam_app_1" || renamed.Start != local[0].Start {
		t.Errorf("renamed session = %+v, want the new name with class and start kept", renamed)
	}

	resized := merged[1]
	want := models.SessionLog{Game: "Deadlock", Class: "deadlock.exe", Start: "2026-10-17T20:10:00-04:00",
		End: "2026-10-17T20:55:00-04:00", DurationSeconds: 2700, Platform: "PC", LogbookID: "resized"}
	if resized != want {
		t.Errorf("resized session = %+v, want %+v", resized, want)
	}

	if merged[2] != local[3] {
		t.Errorf("unlinked session = %+v, want it kept for uploading", merged[2])
	}
}

func TestMergeAddsRowsFromTheSiteAndLinksUnansweredUploads(t *testing.T) {
	local := []models.SessionLog{{
		Game: "RimWorld", Class: "RimWorldLinux", Start: "2026-10-16T20:00:00-04:00", End: "2026-10-16T21:00:20-04:00",
		DurationSeconds: 3620,
	}}
	rows := []Row{
		{ID: "uploaded", OccurredAt: "2026-10-17T00:00:00.000Z", Data: RowData{Game: "rimworld", Minutes: 60, Platform: "PC"}},
		{ID: "switch", OccurredAt: "2026-10-17T15:30:00.000Z", Data: RowData{Game: "Zelda", Minutes: 40, Platform: "Switch"}},
	}

	merged, changes := Merge(local, rows, newYork)
	if changes != (Changes{Added: 1, Linked: 1}) {
		t.Errorf("changes = %+v, want 1 added and 1 linked", changes)
	}
	if len(merged) != 2 {
		t.Fatalf("merged %d sessions, want 2: %+v", len(merged), merged)
	}
	if merged[0].LogbookID != "uploaded" || merged[0].DurationSeconds != 3620 || merged[0].Class != "RimWorldLinux" {
		t.Errorf("linked session = %+v, want it linked with its exact length and class", merged[0])
	}

	want := models.SessionLog{Game: "Zelda", Start: "2026-10-17T11:30:00-04:00", End: "2026-10-17T12:10:00-04:00",
		DurationSeconds: 2400, Platform: "Switch", LogbookID: "switch"}
	if merged[1] != want {
		t.Errorf("added session = %+v, want %+v", merged[1], want)
	}
}
