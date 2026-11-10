package models

import "time"

// Game represents a game to track
type Game struct {
	Class    string `mapstructure:"class" yaml:"class"`
	Name     string `mapstructure:"name,omitempty" yaml:"name,omitempty"`
	Prefix   bool   `mapstructure:"prefix,omitempty" yaml:"prefix,omitempty"`
	UseTitle bool   `mapstructure:"use_title,omitempty" yaml:"use_title,omitempty"`
}

// DisplayName returns the game's display name, falling back to class if not set
func (g Game) DisplayName() string {
	if g.Name != "" {
		return g.Name
	}
	return g.Class
}

// Session tracks an active game session in memory
type Session struct {
	// Address identifies the window: a Hyprland window address, or
	// "steam:<appid>" for a game the Steam client reported.
	Address   string
	Class     string
	Title     string
	GameName  string
	StartTime time.Time

	// Sleep accounting (see sleepclock). Boot is the boot the session started
	// in, AsleepAtStart the machine's total time asleep when it started, and
	// Paused the time asleep counted so far.
	Boot          string
	AsleepAtStart time.Duration
	Paused        time.Duration
}

// Played is how long the game was played if the session ends at end: the
// wall-clock time since it started, less the time spent asleep.
func (s Session) Played(end time.Time) time.Duration {
	played := end.Round(0).Sub(s.StartTime.Round(0)) - s.Paused
	if played < 0 {
		return 0
	}
	return played
}

// SessionLog represents a completed session for persistent storage
type SessionLog struct {
	Game            string `json:"game"`
	Class           string `json:"class"`
	Start           string `json:"start"`
	End             string `json:"end"`
	DurationSeconds int64  `json:"duration_seconds"`

	// Set once the session is synced with Logbook: the id of its row there,
	// and the platform the row names. Sessions added on the site have no class.
	Platform  string `json:"platform,omitempty"`
	LogbookID string `json:"logbook_id,omitempty"`
}

// StartTime parses the session's recorded start timestamp.
func (s SessionLog) StartTime() (time.Time, error) {
	return time.Parse(time.RFC3339, s.Start)
}

// EndTime parses the session's recorded end timestamp.
func (s SessionLog) EndTime() (time.Time, error) {
	return time.Parse(time.RFC3339, s.End)
}

// Duration returns the recorded session length.
func (s SessionLog) Duration() time.Duration {
	return time.Duration(s.DurationSeconds) * time.Second
}

// Settings holds application settings
type Settings struct {
	Notifications  bool   `mapstructure:"notifications" yaml:"notifications"`
	LogSessions    bool   `mapstructure:"log_sessions" yaml:"log_sessions"`
	SessionsFile   string `mapstructure:"sessions_file" yaml:"sessions_file"`
	HyprlandConf   string `mapstructure:"hyprland_conf" yaml:"hyprland_conf"`
	StateFile      string `mapstructure:"state_file" yaml:"state_file,omitempty"`
	MinSessionMins int    `mapstructure:"min_session_mins" yaml:"min_session_mins,omitempty"`
	// Source is where games are seen: "hyprland" (window events), "steam"
	// (the Steam client's game log, for the Steam Deck), or "auto", which
	// picks Hyprland when it's running and Steam otherwise.
	Source string `mapstructure:"source" yaml:"source,omitempty"`
	// SteamDir is Steam's data folder, if it isn't in one of the usual places.
	SteamDir string `mapstructure:"steam_dir" yaml:"steam_dir,omitempty"`
}

// Logbook configures syncing sessions with a sheet on a Logbook site. The
// service token lives in CredentialsFile, never in the config itself.
type Logbook struct {
	Enabled         bool   `mapstructure:"enabled" yaml:"enabled"`
	URL             string `mapstructure:"url" yaml:"url"`
	Sheet           string `mapstructure:"sheet" yaml:"sheet"`
	CredentialsFile string `mapstructure:"credentials_file" yaml:"credentials_file"`
	// Platform is what the sheet's rows say sessions from this machine were
	// played on: PC, Steam Deck, Switch or Other.
	Platform string `mapstructure:"platform" yaml:"platform"`
}

// Config represents the full configuration structure
type Config struct {
	Games    []Game   `mapstructure:"games"`
	Settings Settings `mapstructure:"settings"`
	Logbook  Logbook  `mapstructure:"logbook"`
}
