package hyprland

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/austincgause/gametrak/internal/models"
)

// GenerateGamesConf creates the Hyprland variable file with the game regex
func GenerateGamesConf(games []models.Game, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	regex := BuildGameRegex(games)
	content := fmt.Sprintf("$game_regex = %s\n", regex)

	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write games.conf: %w", err)
	}

	return nil
}

// BuildGameRegex constructs a regex pattern from the game list
func BuildGameRegex(games []models.Game) string {
	if len(games) == 0 {
		return "^$"
	}

	var patterns []string
	for _, game := range games {
		pattern := regexp.QuoteMeta(game.Class)
		if game.Prefix {
			pattern += ".*"
		}
		patterns = append(patterns, pattern)
	}

	return "^(" + strings.Join(patterns, "|") + ")$"
}
