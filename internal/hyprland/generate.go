package hyprland

import (
	"fmt"
	"os"
	"path/filepath"
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
		pattern := escapeRegex(game.Class)
		if game.Prefix {
			pattern += ".*"
		}
		patterns = append(patterns, pattern)
	}

	return "^(" + strings.Join(patterns, "|") + ")$"
}

// escapeRegex escapes special regex characters in a string
func escapeRegex(s string) string {
	special := []string{"\\", ".", "+", "*", "?", "(", ")", "[", "]", "{", "}", "^", "$", "|"}
	result := s
	for _, char := range special {
		// Don't escape if it's already part of the intended pattern
		if char == "." {
			result = strings.ReplaceAll(result, char, "\\"+char)
		}
	}
	return result
}
