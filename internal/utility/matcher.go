package utility

import (
	"strings"

	"github.com/austincgause/gametrak/internal/models"
)

// MatchGame checks if a window class matches any configured game pattern.
// Returns the matched game and true if found, otherwise empty game and false.
func MatchGame(class string, games []models.Game) (models.Game, bool) {
	for _, game := range games {
		if game.Prefix {
			if strings.HasPrefix(class, game.Class) {
				return game, true
			}
		} else {
			if class == game.Class {
				return game, true
			}
		}
	}
	return models.Game{}, false
}
