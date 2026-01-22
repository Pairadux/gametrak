package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Load reads the configuration from viper into the provided Config struct
func Load(cfg *models.Config) error {
	if err := viper.Unmarshal(cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Apply defaults for empty settings
	if cfg.Settings.SessionsFile == "" {
		cfg.Settings.SessionsFile = DefaultSessions
	}
	if cfg.Settings.HyprlandConf == "" {
		cfg.Settings.HyprlandConf = DefaultHyprConf
	}

	return nil
}

// EnsureConfigExists creates the default config file if it doesn't exist
func EnsureConfigExists() error {
	if _, err := os.Stat(DefaultConfigFile); err == nil {
		return nil // Config exists
	}

	if err := os.MkdirAll(DefaultConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	cfg := DefaultConfig()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal default config: %w", err)
	}

	if err := os.WriteFile(DefaultConfigFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// AddGame adds a new game to the configuration and saves it
func AddGame(game models.Game) error {
	var cfg models.Config
	if err := Load(&cfg); err != nil {
		return err
	}

	// Check for duplicate
	for _, existing := range cfg.Games {
		if existing.Class == game.Class {
			return fmt.Errorf("game with class %q already exists", game.Class)
		}
	}

	cfg.Games = append(cfg.Games, game)
	return Save(&cfg)
}

// Save writes the configuration to the config file
func Save(cfg *models.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		configFile = DefaultConfigFile
	}

	if err := os.MkdirAll(filepath.Dir(configFile), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(configFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
