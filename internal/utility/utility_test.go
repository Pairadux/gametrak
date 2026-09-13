package utility

import (
	"strings"
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/models"
)

func TestSanitizeTitle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text is untouched", "RimWorld", "RimWorld"},
		{"normal spaces survive", "Slay the Spire 2", "Slay the Spire 2"},
		{"zero width space is stripped", "ARC\u200bRaiders", "ARCRaiders"},
		{"byte order mark is stripped", "\ufeffARC Raiders", "ARC Raiders"},
		{"zero width joiner is stripped", "ARC\u200dRaiders", "ARCRaiders"},
		{"newlines are stripped", "ARC\nRaiders", "ARCRaiders"},
		{"punctuation survives", "Baldur's Gate 3: Patch", "Baldur's Gate 3: Patch"},
		{"non-ascii letters survive", "Ōkami HD", "Ōkami HD"},
		{"empty stays empty", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeTitle(tc.input); got != tc.want {
				t.Errorf("SanitizeTitle(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestFormatDurationExact(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m 30s"},
		{time.Hour + 2*time.Minute + 3*time.Second, "1h 2m 3s"},
		{0, "0s"},
	}

	for _, tc := range tests {
		if got := FormatDurationExact(tc.duration); got != tc.want {
			t.Errorf("FormatDurationExact(%v) = %q, want %q", tc.duration, got, tc.want)
		}
	}
}

func TestFormatDurationRounded(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{8 * time.Minute, "8 mins"},
		{time.Minute, "1 min"},
		{14 * time.Minute, "14 mins"},
		{20 * time.Minute, "15 mins"},
		{23 * time.Minute, "30 mins"},
		{time.Hour, "1 hour"},
		{time.Hour + 30*time.Minute, "1 hour 30 mins"},
		{2*time.Hour + 15*time.Minute, "2 hours 15 mins"},
		{0, "0 mins"},
	}

	for _, tc := range tests {
		if got := FormatDurationRounded(tc.duration); got != tc.want {
			t.Errorf("FormatDurationRounded(%v) = %q, want %q", tc.duration, got, tc.want)
		}
	}
}

func TestDurationCells(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     []string
	}{
		{"minutes only leaves the hour cells empty", 30 * time.Minute, []string{"", "", "30", "mins"}},
		{"whole hours leave the minute cells empty", 2 * time.Hour, []string{"2", "hours", "", ""}},
		{"singular hour", time.Hour, []string{"1", "hour", "", ""}},
		{"singular minute", time.Minute, []string{"", "", "1", "min"}},
		{"both parts", time.Hour + 30*time.Minute, []string{"1", "hour", "30", "mins"}},
		{"zero still reads as minutes", 0, []string{"", "", "0", "mins"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DurationCells(tc.duration)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("DurationCells(%v) = %q, want %q", tc.duration, got, tc.want)
			}
		})
	}
}

func TestFormatDelta(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{time.Hour, "+1 hour"},
		{-90 * time.Minute, "-1 hour 30 mins"},
		{0, "no change"},
	}

	for _, tc := range tests {
		if got := FormatDelta(tc.duration); got != tc.want {
			t.Errorf("FormatDelta(%v) = %q, want %q", tc.duration, got, tc.want)
		}
	}
}

func TestPlural(t *testing.T) {
	tests := []struct {
		count int
		noun  string
		want  string
	}{
		{1, "session", "1 session"},
		{2, "session", "2 sessions"},
		{0, "day", "0 days"},
	}

	for _, tc := range tests {
		if got := Plural(tc.count, tc.noun); got != tc.want {
			t.Errorf("Plural(%d, %q) = %q, want %q", tc.count, tc.noun, got, tc.want)
		}
	}
}

func TestMatchGame(t *testing.T) {
	games := []models.Game{
		{Class: "steam_app_", Name: "Steam Games", Prefix: true},
		{Class: "RimWorldLinux", Name: "RimWorld"},
		{Class: "deadlock.exe", Name: "Deadlock"},
	}

	tests := []struct {
		name    string
		class   string
		want    string
		matched bool
	}{
		{"exact match", "RimWorldLinux", "RimWorld", true},
		{"prefix match", "steam_app_1422450", "Steam Games", true},
		{"prefix pattern itself", "steam_app_", "Steam Games", true},
		{"exact patterns do not match prefixes", "RimWorldLinux.x86", "", false},
		{"unknown class", "firefox", "", false},
		{"match is case sensitive", "rimworldlinux", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			game, matched := MatchGame(tc.class, games)
			if matched != tc.matched {
				t.Fatalf("MatchGame(%q) matched = %v, want %v", tc.class, matched, tc.matched)
			}
			if matched && game.DisplayName() != tc.want {
				t.Errorf("MatchGame(%q) = %q, want %q", tc.class, game.DisplayName(), tc.want)
			}
		})
	}
}

func TestDisplayNameFallsBackToClass(t *testing.T) {
	game := models.Game{Class: "Terraria.bin.x86_64"}
	if got := game.DisplayName(); got != "Terraria.bin.x86_64" {
		t.Errorf("DisplayName() = %q, want the class", got)
	}
}
