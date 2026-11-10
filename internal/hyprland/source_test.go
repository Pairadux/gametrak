package hyprland

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/austincgause/gametrak/internal/source"
)

func TestSourceTurnsEventLinesIntoEvents(t *testing.T) {
	steamNames := func(class string) string {
		if class == "steam_app_1145360" {
			return "Hades II"
		}
		return ""
	}
	ours, theirs := net.Pipe()
	s := &Source{conn: ours, properName: steamNames}

	events := make(chan source.Event)
	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background(), events) }()

	go func() {
		theirs.Write([]byte("activewindow>>kitty,~\n"))
		theirs.Write([]byte("openwindow>>0xa1,1,steam_app_1145360,Hades II  \n"))
		theirs.Write([]byte("openwindow>>b2,2,RimWorldLinux,RimWorld\n"))
		theirs.Write([]byte("closewindow>>a1\n"))
		theirs.Close() // Hyprland exiting
	}()

	want := []source.Event{
		{Kind: source.Opened, Window: source.Window{ID: "a1", Class: "steam_app_1145360", Title: "Hades II  ", Name: "Hades II"}},
		{Kind: source.Opened, Window: source.Window{ID: "b2", Class: "RimWorldLinux", Title: "RimWorld"}},
		{Kind: source.Closed, Window: source.Window{ID: "a1"}},
	}
	for i, w := range want {
		select {
		case got := <-events:
			if got != w {
				t.Errorf("event %d = %+v, want %+v", i, got, w)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("event %d never arrived", i)
		}
	}
	if err := <-done; err != nil {
		t.Errorf("Run = %v, want nil when Hyprland closes the socket", err)
	}
}

func TestSourceStopsWhenTheContextEnds(t *testing.T) {
	ours, theirs := net.Pipe()
	defer theirs.Close()
	s := &Source{conn: ours}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, make(chan source.Event)) }()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v, want nil after cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
