package hyprland

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// GetSocketPath builds the Hyprland event socket path from environment variables
func GetSocketPath() (string, error) {
	return socketPath(".socket2.sock")
}

// GetCommandSocketPath builds the Hyprland request socket path, used for
// querying state such as the list of open windows.
func GetCommandSocketPath() (string, error) {
	return socketPath(".socket.sock")
}

func socketPath(name string) (string, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return "", fmt.Errorf("XDG_RUNTIME_DIR not set")
	}

	instanceSig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if instanceSig == "" {
		return "", fmt.Errorf("HYPRLAND_INSTANCE_SIGNATURE not set (is Hyprland running?)")
	}

	return filepath.Join(runtimeDir, "hypr", instanceSig, name), nil
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
