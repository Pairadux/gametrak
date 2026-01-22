package hyprland

import "strings"

// Event types
const (
	EventOpenWindow  = "openwindow"
	EventCloseWindow = "closewindow"
)

// OpenWindowEvent represents a parsed openwindow event
type OpenWindowEvent struct {
	Address   string
	Workspace string
	Class     string
	Title     string
}

// CloseWindowEvent represents a parsed closewindow event
type CloseWindowEvent struct {
	Address string
}

// ParseEvent extracts the event type and data from a raw event line
func ParseEvent(line string) (eventType string, data string, ok bool) {
	parts := strings.SplitN(line, ">>", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// ParseOpenWindow parses the data portion of an openwindow event.
// Format: ADDRESS,WORKSPACE,CLASS,TITLE
func ParseOpenWindow(data string) (OpenWindowEvent, bool) {
	parts := strings.SplitN(data, ",", 4)
	if len(parts) < 3 {
		return OpenWindowEvent{}, false
	}

	event := OpenWindowEvent{
		Address:   parts[0],
		Workspace: parts[1],
		Class:     parts[2],
	}

	if len(parts) >= 4 {
		event.Title = parts[3]
	}

	return event, true
}

// ParseCloseWindow parses the data portion of a closewindow event.
// Format: ADDRESS
func ParseCloseWindow(data string) (CloseWindowEvent, bool) {
	address := strings.TrimSpace(data)
	if address == "" {
		return CloseWindowEvent{}, false
	}
	return CloseWindowEvent{Address: address}, true
}
