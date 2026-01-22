/*
Copyright © 2026 Austin Gause
*/
package cmd

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// Session tracks an active game session
type Session struct {
	Address   string
	Class     string
	Title     string
	StartTime time.Time
}

// Hardcoded game patterns for MVP
// Patterns ending with underscore are treated as prefixes
var gamePatterns = []string{
	"steam_app_",    // Steam games (prefix match)
	"RimWorldLinux", // RimWorld
	"deadlock.exe",  // Deadlock
}

// Active game sessions tracked by window address
var activeSessions = make(map[string]*Session)

// shutdownRequested tracks if we're in graceful shutdown
var shutdownRequested bool

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gametrak",
	Short: "Event-driven game session tracker for Hyprland",
	Long: `Gametrak is a lightweight game session tracker that listens to
Hyprland's IPC event socket to detect when games start and stop.

It tracks session durations and outputs events to stdout.
Running gametrak without subcommands starts the monitoring service.`,
	Run: func(cmd *cobra.Command, args []string) {
		runMonitor()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

var debugMode bool

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/gametrak/config.yaml)")
	rootCmd.Flags().BoolVarP(&debugMode, "debug", "d", false, "print all Hyprland events for debugging")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		configDir, err := os.UserConfigDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(filepath.Join(configDir, "gametrak"))
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName("config")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

// runMonitor starts the Hyprland event monitoring loop
func runMonitor() {
	socketPath, err := getSocketPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[%s] Connecting to Hyprland socket: %s\n", timestamp(), socketPath)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to connect to Hyprland socket: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Printf("[%s] Connected. Listening for game events...\n", timestamp())
	fmt.Printf("[%s] Watching for patterns: %v\n", timestamp(), gamePatterns)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		shutdownRequested = true
		fmt.Printf("\n[%s] Shutting down...\n", timestamp())
		conn.Close()
	}()

	// Read events from socket
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		handleEvent(line)
	}

	if err := scanner.Err(); err != nil && !shutdownRequested {
		fmt.Fprintf(os.Stderr, "Error reading from socket: %v\n", err)
		os.Exit(1)
	}
}

// getSocketPath builds the Hyprland socket2 path from environment variables
func getSocketPath() (string, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return "", fmt.Errorf("XDG_RUNTIME_DIR not set")
	}

	instanceSig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if instanceSig == "" {
		return "", fmt.Errorf("HYPRLAND_INSTANCE_SIGNATURE not set (is Hyprland running?)")
	}

	return filepath.Join(runtimeDir, "hypr", instanceSig, ".socket2.sock"), nil
}

// handleEvent parses and routes Hyprland events
func handleEvent(line string) {
	if debugMode {
		fmt.Printf("[%s] DEBUG: %s\n", timestamp(), line)
	}

	parts := strings.SplitN(line, ">>", 2)
	if len(parts) != 2 {
		return
	}

	event := parts[0]
	data := parts[1]

	switch event {
	case "openwindow":
		handleOpenWindow(data)
	case "closewindow":
		handleCloseWindow(data)
	}
}

// handleOpenWindow processes openwindow events
// Format: ADDRESS,CLASS,TITLE
func handleOpenWindow(data string) {
	parts := strings.SplitN(data, ",", 3)
	if len(parts) < 2 {
		return
	}

	address := parts[0]
	class := parts[1]
	title := ""
	if len(parts) >= 3 {
		title = parts[2]
	}

	if !matchesGame(class) {
		return
	}

	session := &Session{
		Address:   address,
		Class:     class,
		Title:     title,
		StartTime: time.Now(),
	}
	activeSessions[address] = session

	displayName := title
	if displayName == "" {
		displayName = class
	}

	fmt.Printf("[%s] Game started: %s (class: %s, address: %s)\n",
		timestamp(), displayName, class, address)
}

// handleCloseWindow processes closewindow events
// Format: ADDRESS
func handleCloseWindow(data string) {
	address := strings.TrimSpace(data)

	session, exists := activeSessions[address]
	if !exists {
		return
	}

	duration := time.Since(session.StartTime)
	delete(activeSessions, address)

	displayName := session.Title
	if displayName == "" {
		displayName = session.Class
	}

	fmt.Printf("[%s] Game ended: %s - Session: %s\n",
		timestamp(), displayName, formatDuration(duration))
}

// matchesGame checks if a window class matches any configured game pattern
func matchesGame(class string) bool {
	for _, pattern := range gamePatterns {
		if strings.HasSuffix(pattern, "_") {
			if strings.HasPrefix(class, pattern) {
				return true
			}
		} else {
			if class == pattern {
				return true
			}
		}
	}
	return false
}

// formatDuration formats a duration as "Xh Xm Xs"
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)

	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// timestamp returns the current time formatted for logging
func timestamp() string {
	return time.Now().Format("15:04:05")
}
