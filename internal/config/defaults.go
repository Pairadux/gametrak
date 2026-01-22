package config

import (
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/austincgause/gametrak/internal/models"
)

var (
	DefaultConfigDir  = filepath.Join(xdg.ConfigHome, "gametrak")
	DefaultDataDir    = filepath.Join(xdg.DataHome, "gametrak")
	DefaultConfigFile = filepath.Join(DefaultConfigDir, "config.yaml")
	DefaultSessions   = filepath.Join(DefaultDataDir, "sessions.jsonl")
	DefaultHyprConf   = filepath.Join(DefaultConfigDir, "games.conf")
)

// DefaultGames returns the default game patterns
func DefaultGames() []models.Game {
	return []models.Game{
		{Class: "steam_app_", Name: "Steam Games", Prefix: true},
		{Class: "deadlock.exe", Name: "Deadlock"},
		{Class: "hl2_linux", Name: "Half-Life 2"},
		{Class: "RimWorldLinux", Name: "RimWorld"},
		{Class: "FishingPlanet.X86_64", Name: "Fishing Planet"},
		{Class: "rocketleague", Name: "Rocket League", Prefix: true},
	}
}

// DefaultSettings returns default application settings
func DefaultSettings() models.Settings {
	return models.Settings{
		Notifications: true,
		LogSessions:   true,
		SessionsFile:  DefaultSessions,
		HyprlandConf:  DefaultHyprConf,
	}
}

// DefaultConfig returns a complete default configuration
func DefaultConfig() models.Config {
	return models.Config{
		Games:    DefaultGames(),
		Settings: DefaultSettings(),
	}
}
