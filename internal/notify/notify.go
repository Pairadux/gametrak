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

	return send(title, body)
}

func send(title, body string) error {
	cmd := exec.Command("notify-send", title, body)
	return cmd.Run()
}
