// Package source defines what the tracker needs from wherever it learns about
// games: Hyprland's window events on the laptop, or Steam's game log on the
// Steam Deck.
package source

import (
	"context"
	"time"
)

// Window is something that may be a game: a Hyprland window, or a game the
// Steam client reports as running.
type Window struct {
	// ID tells this window apart from every other open one and matches its
	// Closed event: a Hyprland window address, or "steam:<appid>".
	ID    string
	Class string
	Title string
	// Name is the game's proper name when the source knows it, such as the
	// name Steam lists for the app. Empty otherwise.
	Name string
	// Game is set by sources that only ever report games, so the window
	// counts without a matching entry in the config's game list.
	Game bool
}

// Kind says whether an event opens or closes a window.
type Kind int

const (
	Opened Kind = iota + 1
	Closed
)

// Event is a window opening or closing. A Closed event only sets Window.ID.
type Event struct {
	Kind   Kind
	Window Window
	// At is when the window opened, for a source that reports it a moment
	// later (the Steam source waits to learn whose game it is). Zero means
	// as the event arrives.
	At time.Time
}

// Source reports games starting and stopping.
type Source interface {
	// Name describes the source in the daemon's log, e.g. "Hyprland".
	Name() string
	// Snapshot lists what is open right now, so the tracker can reconcile
	// with a previous run. Call it before Run.
	Snapshot() ([]Window, error)
	// Run delivers events until ctx is done or the source ends. It returns
	// nil when ctx is done or the source ended normally (Hyprland exiting),
	// and an error when the source failed.
	Run(ctx context.Context, events chan<- Event) error
	// Close releases the source's connections and watches.
	Close() error
}
