package utility

import (
	"strings"
	"unicode"
)

// SanitizeTitle strips invisible Unicode characters (zero-width spaces, BOM, etc.)
// from a string while preserving all visible characters and normal spaces.
func SanitizeTitle(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsGraphic(r) {
			return r
		}
		return -1
	}, s)
}
