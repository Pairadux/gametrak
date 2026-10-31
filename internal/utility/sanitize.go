package utility

import (
	"strings"
	"unicode"
)

// SanitizeTitle strips invisible Unicode characters (zero-width spaces, BOM, etc.)
// and surrounding whitespace from a string, while preserving all visible
// characters and the spaces between words. Some Steam games pad their window
// titles with trailing spaces, which Logbook trims, so without the trim the
// same game would be named two ways.
func SanitizeTitle(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsGraphic(r) {
			return r
		}
		return -1
	}, s))
}
