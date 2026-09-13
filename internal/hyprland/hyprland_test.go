package hyprland

import (
	"testing"

	"github.com/austincgause/gametrak/internal/models"
)

func TestParseEvent(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		eventType string
		data      string
		ok        bool
	}{
		{"open window", "openwindow>>abc,1,RimWorldLinux,RimWorld", "openwindow", "abc,1,RimWorldLinux,RimWorld", true},
		{"close window", "closewindow>>abc", "closewindow", "abc", true},
		{"data containing the separator", "openwindow>>abc,1,x,a>>b", "openwindow", "abc,1,x,a>>b", true},
		{"missing separator", "workspace", "", "", false},
		{"empty line", "", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eventType, data, ok := ParseEvent(tc.line)
			if ok != tc.ok {
				t.Fatalf("ParseEvent(%q) ok = %v, want %v", tc.line, ok, tc.ok)
			}
			if eventType != tc.eventType || data != tc.data {
				t.Errorf("ParseEvent(%q) = (%q, %q), want (%q, %q)", tc.line, eventType, data, tc.eventType, tc.data)
			}
		})
	}
}

func TestParseOpenWindow(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		want  OpenWindowEvent
		valid bool
	}{
		{
			name:  "full event",
			data:  "5f3a,1,RimWorldLinux,RimWorld - v1.5",
			want:  OpenWindowEvent{Address: "5f3a", Workspace: "1", Class: "RimWorldLinux", Title: "RimWorld - v1.5"},
			valid: true,
		},
		{
			name:  "title containing commas is kept whole",
			data:  "5f3a,1,steam_app_1,Game, Chapter 2, Part 3",
			want:  OpenWindowEvent{Address: "5f3a", Workspace: "1", Class: "steam_app_1", Title: "Game, Chapter 2, Part 3"},
			valid: true,
		},
		{
			name:  "missing title",
			data:  "5f3a,1,RimWorldLinux",
			want:  OpenWindowEvent{Address: "5f3a", Workspace: "1", Class: "RimWorldLinux"},
			valid: true,
		},
		{
			name:  "address prefix is normalized away",
			data:  "0x5f3a,1,RimWorldLinux,RimWorld",
			want:  OpenWindowEvent{Address: "5f3a", Workspace: "1", Class: "RimWorldLinux", Title: "RimWorld"},
			valid: true,
		},
		{name: "too few fields", data: "5f3a,1", valid: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseOpenWindow(tc.data)
			if ok != tc.valid {
				t.Fatalf("ParseOpenWindow(%q) ok = %v, want %v", tc.data, ok, tc.valid)
			}
			if ok && got != tc.want {
				t.Errorf("ParseOpenWindow(%q) = %+v, want %+v", tc.data, got, tc.want)
			}
		})
	}
}

func TestParseCloseWindow(t *testing.T) {
	if got, ok := ParseCloseWindow("0x5f3a"); !ok || got.Address != "5f3a" {
		t.Errorf("ParseCloseWindow = (%+v, %v), want address 5f3a", got, ok)
	}
	if _, ok := ParseCloseWindow("  "); ok {
		t.Error("ParseCloseWindow accepted a blank address")
	}
}

func TestBuildGameRegex(t *testing.T) {
	tests := []struct {
		name  string
		games []models.Game
		want  string
	}{
		{
			name:  "no games",
			games: nil,
			want:  "^$",
		},
		{
			name:  "dots are escaped",
			games: []models.Game{{Class: "deadlock.exe"}},
			want:  `^(deadlock\.exe)$`,
		},
		{
			name:  "prefix patterns match anything after them",
			games: []models.Game{{Class: "steam_app_", Prefix: true}},
			want:  "^(steam_app_.*)$",
		},
		{
			// A class containing regex metacharacters must be matched literally.
			name:  "all metacharacters are escaped",
			games: []models.Game{{Class: "a+b(c)|d[e]"}},
			want:  `^(a\+b\(c\)\|d\[e\])$`,
		},
		{
			name:  "multiple games are alternatives",
			games: []models.Game{{Class: "hl2_linux"}, {Class: "rocketleague", Prefix: true}},
			want:  "^(hl2_linux|rocketleague.*)$",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildGameRegex(tc.games); got != tc.want {
				t.Errorf("BuildGameRegex = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeAddress(t *testing.T) {
	for input, want := range map[string]string{
		"0x5f3a": "5f3a",
		"5f3a":   "5f3a",
		" 5f3a ": "5f3a",
		"":       "",
	} {
		if got := NormalizeAddress(input); got != want {
			t.Errorf("NormalizeAddress(%q) = %q, want %q", input, got, want)
		}
	}
}
