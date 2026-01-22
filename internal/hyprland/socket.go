package hyprland

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// GetSocketPath builds the Hyprland socket2 path from environment variables
func GetSocketPath() (string, error) {
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

// Connect establishes a connection to the Hyprland event socket
func Connect() (net.Conn, error) {
	socketPath, err := GetSocketPath()
	if err != nil {
		return nil, err
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Hyprland socket: %w", err)
	}

	return conn, nil
}

// Listen reads events from the connection and sends them to the provided channel.
// It blocks until the connection is closed or an error occurs.
func Listen(conn net.Conn, events chan<- string, errors chan<- error) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		events <- scanner.Text()
	}

	if err := scanner.Err(); err != nil {
		errors <- err
	}
	close(events)
}
