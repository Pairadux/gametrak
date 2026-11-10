package hyprland

import (
	"bufio"
	"context"
	"fmt"
	"net"

	"github.com/austincgause/gametrak/internal/source"
)

// Source reports windows from Hyprland's IPC sockets: the window list for a
// snapshot, and open and close events from the event socket.
type Source struct {
	conn net.Conn
	// properName, when set, gives a window class's proper game name, such as
	// Steam's name for a steam_app_<id> window, or "".
	properName func(class string) string
	debugf     func(format string, args ...any)
}

// Open connects to Hyprland's event socket. properName and debugf may be nil.
func Open(properName func(class string) string, debugf func(format string, args ...any)) (*Source, error) {
	conn, err := Connect()
	if err != nil {
		return nil, err
	}
	return &Source{conn: conn, properName: properName, debugf: debugf}, nil
}

// Name describes the source in the daemon's log.
func (s *Source) Name() string { return "Hyprland" }

// Snapshot lists the windows open right now.
func (s *Source) Snapshot() ([]source.Window, error) {
	clients, err := Clients()
	if err != nil {
		return nil, err
	}
	windows := make([]source.Window, 0, len(clients))
	for _, c := range clients {
		windows = append(windows, s.window(c.Address, c.Class, c.Title))
	}
	return windows, nil
}

// Run reads the event socket until Hyprland closes it, which means Hyprland is
// shutting down and its windows with it, or until ctx is done.
func (s *Source) Run(ctx context.Context, events chan<- source.Event) error {
	stop := context.AfterFunc(ctx, func() { s.conn.Close() })
	defer stop()

	scanner := bufio.NewScanner(s.conn)
	for scanner.Scan() {
		line := scanner.Text()
		if s.debugf != nil {
			s.debugf("DEBUG: %s", line)
		}
		event, ok := s.parse(line)
		if !ok {
			continue
		}
		select {
		case events <- event:
		case <-ctx.Done():
			return nil
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading from Hyprland's event socket: %w", err)
	}
	return nil
}

// Close disconnects from the event socket.
func (s *Source) Close() error {
	return s.conn.Close()
}

// parse turns an event socket line into an event, if it is one gametrak uses.
func (s *Source) parse(line string) (source.Event, bool) {
	eventType, data, ok := ParseEvent(line)
	if !ok {
		return source.Event{}, false
	}
	switch eventType {
	case EventOpenWindow:
		if e, ok := ParseOpenWindow(data); ok {
			return source.Event{Kind: source.Opened, Window: s.window(e.Address, e.Class, e.Title)}, true
		}
	case EventCloseWindow:
		if e, ok := ParseCloseWindow(data); ok {
			return source.Event{Kind: source.Closed, Window: source.Window{ID: e.Address}}, true
		}
	}
	return source.Event{}, false
}

func (s *Source) window(address, class, title string) source.Window {
	w := source.Window{ID: address, Class: class, Title: title}
	if s.properName != nil {
		w.Name = s.properName(class)
	}
	return w
}
