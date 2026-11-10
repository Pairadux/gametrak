package utility

import (
	"strings"

	"github.com/austincgause/gametrak/internal/models"
)

// MatchGame checks if a window class matches any configured game pattern.
// Returns the matched game and true if found, otherwise empty game and false.
// An entry for exactly this class wins over a prefix that also matches,
// wherever each sits in the list, so one game can be named on its own while
// a prefix covers the rest.
func MatchGame(class string, games []models.Game) (models.Game, bool) {
	for _, game := range games {
		if !game.Prefix && class == game.Class {
			return game, true
		}
	}
	for _, game := range games {
		if game.Prefix && strings.HasPrefix(class, game.Class) {
			return game, true
		}
	}
	return models.Game{}, false
}
