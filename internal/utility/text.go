package utility

import "fmt"

// Plural formats a count with its noun, adding "s" when the count is not one.
func Plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
