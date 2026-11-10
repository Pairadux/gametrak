package cmd

import (
	"context"
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
	"github.com/austincgause/gametrak/internal/source"
	"github.com/austincgause/gametrak/internal/steam"
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
	Long: `Gametrak is a lightweight game session tracker. On a Hyprland desktop it
listens to Hyprland's IPC event socket to see game windows open and close; on
a Steam Deck it follows the Steam client's log of the games it runs.

It tracks session durations, leaving out time the machine spends asleep, and
outputs events to stdout. Running gametrak without subcommands starts the
monitoring service.`,
	CompletionOptions: cobra.CompletionOptions{
		HiddenDefaultCmd: true,
	},
	// A failed query is a runtime error, not a usage mistake; printing the
	// full help text after it buries the message.
	SilenceUsage: true,
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
	src, err := openSource()
	if err != nil {
		notify.Error(err.Error())
		return err
	}

	if cfg.Settings.Notifications {
		go notify.Started()
	}

	if s, ok := src.(*steam.Source); ok {
		logf("Watching for: every installed Steam game (%d found)", s.Installed())
	} else {
		var names []string
		for _, g := range cfg.Games {
			names = append(names, g.DisplayName())
		}
		logf("Watching for: %s", strings.Join(names, ", "))
	}

	track := tracker.New(cfg, os.Stdout, debugMode)
	stopSync := startSync(track)
	defer stopSync()
	reconcile(track, src)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan source.Event)
	ended := make(chan error, 1)
	go func() { ended <- src.Run(ctx, events) }()

	// stopSource ends the source and waits briefly for it to let go, so
	// nothing is still reading when it's closed.
	stopSource := func() {
		cancel()
		select {
		case <-ended:
		case <-time.After(5 * time.Second):
		}
		src.Close()
	}

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
			stopSource()
			return shutdown()

		case <-heartbeat.C:
			if len(track.Active()) == 0 {
				continue
			}
			if err := track.Persist(time.Now()); err != nil {
				logf("Warning: failed to save state: %v", err)
			}

		case err := <-ended:
			src.Close()
			if err != nil {
				notify.Error(err.Error())
				shutdown()
				return err
			}
			// Hyprland closed its socket, so it is shutting down and any
			// game windows are going with it.
			logf("%s closed", src.Name())
			return shutdown()

		case event := <-events:
			track.Apply(event, time.Now())
		}
	}
}

// openSource connects to wherever this machine's games are seen, as
// settings.source says: Hyprland's window events, or the Steam client's game
// log. "auto" picks Hyprland when it's running, and Steam otherwise.
func openSource() (source.Source, error) {
	kind := strings.ToLower(strings.TrimSpace(cfg.Settings.Source))
	auto := kind == "" || kind == "auto"
	if auto {
		kind = "steam"
		if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
			kind = "hyprland"
		}
	}

	switch kind {
	case "hyprland":
		socketPath, err := hyprland.GetSocketPath()
		if err != nil {
			return nil, err
		}
		logf("Connecting to Hyprland socket: %s", socketPath)
		src, err := hyprland.Open(steamNames(), debugf())
		if err != nil {
			return nil, err
		}
		logf("Connected. Listening for game events...")
		return src, nil

	case "steam":
		root, err := steam.FindRoot(cfg.Settings.SteamDir)
		if err != nil {
			if auto {
				return nil, fmt.Errorf("no way to see games: Hyprland isn't running, and %w", err)
			}
			return nil, err
		}
		logf("Following Steam's game log: %s", steam.GameLogPath(root))
		return steam.Open(root, logf, debugf())
	}
	return nil, fmt.Errorf("unknown settings.source %q (want auto, hyprland or steam)", cfg.Settings.Source)
}

// steamNames gives Steam's own names for Proton games' steam_app_<id>
// windows, so a game is named the same on every machine. It's nil when Steam
// isn't installed here.
func steamNames() func(class string) string {
	root, err := steam.FindRoot(cfg.Settings.SteamDir)
	if err != nil {
		return nil
	}
	library := steam.NewLibrary(root)
	return func(class string) string {
		id, ok := steam.AppIDFromClass(class)
		if !ok {
			return ""
		}
		name, _ := library.Name(id)
		return name
	}
}

// reconcile catches up on sessions from a previous run and on games that
// were already running. Failing to list them is not fatal: the daemon still
// tracks everything that starts from here on.
func reconcile(track *tracker.Tracker, src source.Source) {
	open, err := src.Snapshot()
	if err != nil {
		logf("Warning: could not list what's already running: %v", err)
		return
	}
	if err := track.Reconcile(open, time.Now()); err != nil {
		logf("Warning: could not restore previous state: %v", err)
	}
}

// debugf logs like logf with --debug, and is nil otherwise.
func debugf() func(format string, args ...any) {
	if !debugMode {
		return nil
	}
	return logf
}

func logf(format string, args ...any) {
	fmt.Printf("[%s] %s\n", utility.Timestamp(), fmt.Sprintf(format, args...))
}
