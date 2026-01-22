package notify

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/austincgause/gametrak/internal/utility"
)

// GameEnded sends a notification when a game session ends
func GameEnded(gameName string, duration time.Duration) error {
	title := "Game Session Ended"
	body := fmt.Sprintf("%s - %s", gameName, utility.FormatDurationRounded(duration))

	return send(title, body, "normal")
}

// Error sends a notification when gametrak encounters a fatal error
func Error(message string) error {
	return send("Gametrak Error", message, "critical")
}

// Started sends a notification when gametrak starts monitoring
func Started() error {
	return send("Gametrak", "Now tracking game sessions", "low")
}

func send(title, body, urgency string) error {
	cmd := exec.Command("notify-send", "-u", urgency, title, body)
	return cmd.Run()
}
