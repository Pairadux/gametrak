package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/austincgause/gametrak/internal/config"
	"github.com/austincgause/gametrak/internal/hyprland"
	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/notify"
	"github.com/austincgause/gametrak/internal/tracker"
	"github.com/austincgause/gametrak/internal/utility"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// heartbeatInterval bounds how much playtime can be lost if the daemon is
// killed without a chance to shut down: the state file is refreshed this often
// while games are running, and its timestamp becomes the recovered end time.
const heartbeatInterval = time.Minute

var (
	cfg       models.Config
	cfgFile   string
	debugMode bool
)

var rootCmd = &cobra.Command{
	Use:   "gametrak",
	Short: "Event-driven game session tracker for Hyprland",
	Long: `Gametrak is a lightweight game session tracker that listens to
Hyprland's IPC event socket to detect when games start and stop.

It tracks session durations and outputs events to stdout.
Running gametrak without subcommands starts the monitoring service.`,
	CompletionOptions: cobra.CompletionOptions{
		HiddenDefaultCmd: true,
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip config loading for commands that don't need it
		if cmd.Name() == "help" {
			return nil
		}
		return config.Load(&cfg)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMonitor()
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

func runMonitor() error {
	socketPath, err := hyprland.GetSocketPath()
	if err != nil {
		notify.Error(err.Error())
		return err
	}

	logf("Connecting to Hyprland socket: %s", socketPath)

	conn, err := hyprland.Connect()
	if err != nil {
		notify.Error(err.Error())
		return err
	}
	defer conn.Close()

	logf("Connected. Listening for game events...")

	if cfg.Settings.Notifications {
		notify.Started()
	}

	var names []string
	for _, g := range cfg.Games {
		names = append(names, g.DisplayName())
	}
	logf("Watching for: %s", strings.Join(names, ", "))

	track := tracker.New(cfg, os.Stdout, debugMode)
	reconcile(track)

	events := make(chan string)
	errors := make(chan error)
	go hyprland.Listen(conn, events, errors)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	shutdown := func() error {
		if err := track.Shutdown(time.Now()); err != nil {
			return fmt.Errorf("failed to save active sessions: %w", err)
		}
		return nil
	}

	for {
		select {
		case <-signals:
			fmt.Println()
			logf("Shutting down...")
			conn.Close()
			return shutdown()

		case <-heartbeat.C:
			if len(track.Active()) == 0 {
				continue
			}
			if err := track.Persist(time.Now()); err != nil {
				logf("Warning: failed to save state: %v", err)
			}

		case err := <-errors:
			notify.Error(fmt.Sprintf("Socket error: %v", err))
			shutdown()
			return fmt.Errorf("error reading from socket: %w", err)

		case line, ok := <-events:
			if !ok {
				// Hyprland closed the socket, so it is shutting down and any
				// game windows are going with it.
				logf("Hyprland socket closed")
				return shutdown()
			}
			track.HandleEvent(line, time.Now())
		}
	}
}

// reconcile catches up on sessions from a previous run and on game windows
// that were already open. Failing to reach Hyprland is not fatal: the daemon
// still tracks everything that opens from here on.
func reconcile(track *tracker.Tracker) {
	clients, err := hyprland.Clients()
	if err != nil {
		logf("Warning: could not query open windows: %v", err)
		return
	}
	if err := track.Reconcile(clients, time.Now()); err != nil {
		logf("Warning: could not restore previous state: %v", err)
	}
}

func logf(format string, args ...any) {
	fmt.Printf("[%s] %s\n", utility.Timestamp(), fmt.Sprintf(format, args...))
}
