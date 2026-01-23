package models

import "time"

// Game represents a game to track
type Game struct {
	Class  string `mapstructure:"class" yaml:"class"`
	Name   string `mapstructure:"name,omitempty" yaml:"name,omitempty"`
	Prefix bool   `mapstructure:"prefix,omitempty" yaml:"prefix,omitempty"`
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
	Address   string
	Class     string
	Title     string
	GameName  string
	StartTime time.Time
}

// SessionLog represents a completed session for persistent storage
type SessionLog struct {
	Game            string `json:"game"`
	Class           string `json:"class"`
	Start           string `json:"start"`
	End             string `json:"end"`
	DurationSeconds int64  `json:"duration_seconds"`
}

// Settings holds application settings
type Settings struct {
	Notifications bool   `mapstructure:"notifications" yaml:"notifications"`
	LogSessions   bool   `mapstructure:"log_sessions" yaml:"log_sessions"`
	SessionsFile  string `mapstructure:"sessions_file" yaml:"sessions_file"`
	HyprlandConf  string `mapstructure:"hyprland_conf" yaml:"hyprland_conf"`
}

// Config represents the full configuration structure
type Config struct {
	Games    []Game   `mapstructure:"games"`
	Settings Settings `mapstructure:"settings"`
}
