package hyprland

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// clientTimeout bounds a request to the Hyprland command socket.
const clientTimeout = 3 * time.Second

// Client is an open window as reported by Hyprland.
type Client struct {
	Address string `json:"address"`
	Class   string `json:"class"`
	Title   string `json:"title"`
	PID     int    `json:"pid"`
	Mapped  bool   `json:"mapped"`
}

// Clients queries Hyprland for the currently open windows. Addresses are
// normalized to match the form used by event socket messages.
func Clients() ([]Client, error) {
	data, err := request("j/clients")
	if err != nil {
		return nil, err
	}

	var clients []Client
	if err := json.Unmarshal(data, &clients); err != nil {
		return nil, fmt.Errorf("failed to parse client list: %w", err)
	}

	for i := range clients {
		clients[i].Address = NormalizeAddress(clients[i].Address)
	}
	return clients, nil
}

// NormalizeAddress strips the "0x" prefix Hyprland uses in its JSON replies but
// not in its event stream, so addresses from both sources compare equal.
func NormalizeAddress(address string) string {
	return strings.TrimPrefix(strings.TrimSpace(address), "0x")
}

func request(command string) ([]byte, error) {
	path, err := GetCommandSocketPath()
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout("unix", path, clientTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Hyprland command socket: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(clientTimeout)); err != nil {
		return nil, fmt.Errorf("failed to set socket deadline: %w", err)
	}
	if _, err := conn.Write([]byte(command)); err != nil {
		return nil, fmt.Errorf("failed to send %q: %w", command, err)
	}

	data, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to read reply to %q: %w", command, err)
	}
	return data, nil
}
