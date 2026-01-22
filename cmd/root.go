/*
Copyright © 2026 Austin Gause
*/
package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/austincgause/gametrak/internal/config"
	"github.com/austincgause/gametrak/internal/hyprland"
	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/notify"
	"github.com/austincgause/gametrak/internal/session"
	"github.com/austincgause/gametrak/internal/utility"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfg             models.Config
	cfgFile         string
	debugMode       bool
	activeSessions  = make(map[string]*models.Session)
	shutdownRequest bool
)

var rootCmd = &cobra.Command{
	Use:   "gametrak",
	Short: "Event-driven game session tracker for Hyprland",
	Long: `Gametrak is a lightweight game session tracker that listens to
Hyprland's IPC event socket to detect when games start and stop.

It tracks session durations and outputs events to stdout.
Running gametrak without subcommands starts the monitoring service.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip config loading for commands that don't need it
		if cmd.Name() == "help" {
			return nil
		}
		return config.Load(&cfg)
	},
	Run: func(cmd *cobra.Command, args []string) {
		runMonitor()
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $XDG_CONFIG_HOME/gametrak/config.yaml)")
	rootCmd.Flags().BoolVarP(&debugMode, "debug", "d", false, "print all Hyprland events for debugging")
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(config.DefaultConfigDir)
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName("config")
	}

	viper.AutomaticEnv()

	// Create default config if it doesn't exist
	if err := config.EnsureConfigExists(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create default config: %v\n", err)
	}

	if err := viper.ReadInConfig(); err == nil {
		if debugMode {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
	}
}

func runMonitor() {
	socketPath, err := hyprland.GetSocketPath()
	if err != nil {
		notify.Error(err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[%s] Connecting to Hyprland socket: %s\n", utility.Timestamp(), socketPath)

	conn, err := hyprland.Connect()
	if err != nil {
		notify.Error(err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Printf("[%s] Connected. Listening for game events...\n", utility.Timestamp())

	// Send startup notification
	if cfg.Settings.Notifications {
		notify.Started()
	}

	// Print watched games
	var gameNames []string
	for _, g := range cfg.Games {
		gameNames = append(gameNames, g.DisplayName())
	}
	fmt.Printf("[%s] Watching for: %v\n", utility.Timestamp(), gameNames)

	// Set up channels for events
	events := make(chan string)
	errors := make(chan error)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go hyprland.Listen(conn, events, errors)

	for {
		select {
		case <-sigChan:
			shutdownRequest = true
			fmt.Printf("\n[%s] Shutting down...\n", utility.Timestamp())
			conn.Close()
			return

		case err := <-errors:
			if !shutdownRequest {
				notify.Error(fmt.Sprintf("Socket error: %v", err))
				fmt.Fprintf(os.Stderr, "Error reading from socket: %v\n", err)
				os.Exit(1)
			}
			return

		case line, ok := <-events:
			if !ok {
				return
			}
			handleEvent(line)
		}
	}
}

func handleEvent(line string) {
	if debugMode {
		fmt.Printf("[%s] DEBUG: %s\n", utility.Timestamp(), line)
	}

	eventType, data, ok := hyprland.ParseEvent(line)
	if !ok {
		return
	}

	switch eventType {
	case hyprland.EventOpenWindow:
		handleOpenWindow(data)
	case hyprland.EventCloseWindow:
		handleCloseWindow(data)
	}
}

func handleOpenWindow(data string) {
	event, ok := hyprland.ParseOpenWindow(data)
	if !ok {
		return
	}

	game, matched := utility.MatchGame(event.Class, cfg.Games)
	if !matched {
		return
	}

	sess := &models.Session{
		Address:   event.Address,
		Class:     event.Class,
		Title:     event.Title,
		GameName:  game.DisplayName(),
		StartTime: time.Now(),
	}
	activeSessions[event.Address] = sess

	displayName := event.Title
	if displayName == "" {
		displayName = game.DisplayName()
	}

	fmt.Printf("[%s] Game started: %s (class: %s, address: %s)\n",
		utility.Timestamp(), displayName, event.Class, event.Address)
}

func handleCloseWindow(data string) {
	event, ok := hyprland.ParseCloseWindow(data)
	if !ok {
		return
	}

	sess, exists := activeSessions[event.Address]
	if !exists {
		return
	}

	endTime := time.Now()
	duration := endTime.Sub(sess.StartTime)
	delete(activeSessions, event.Address)

	displayName := sess.Title
	if displayName == "" {
		displayName = sess.GameName
	}

	fmt.Printf("[%s] Game ended: %s - Session: %s\n",
		utility.Timestamp(), displayName, utility.FormatDurationExact(duration))

	// Log session if enabled
	if cfg.Settings.LogSessions {
		if err := session.Log(cfg.Settings.SessionsFile, *sess, endTime); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to log session: %v\n", err)
		}
	}

	// Send notification if enabled
	if cfg.Settings.Notifications {
		if err := notify.GameEnded(displayName, duration); err != nil {
			if debugMode {
				fmt.Fprintf(os.Stderr, "Warning: failed to send notification: %v\n", err)
			}
		}
	}
}
