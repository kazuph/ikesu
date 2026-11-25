package snapshot

import (
	"fmt"
	"unicode/utf8"
)

const DefaultMaxChars = 6000

// Trim keeps roughly one third of the retained runes from the beginning
// and the rest from the end.
func Trim(s string, limit int) string {
	count := utf8.RuneCountInString(s)
	if limit <= 0 {
		return ""
	}
	if count <= limit {
		return s
	}

	runes := []rune(s)
	for kept := max(0, limit-utf8.RuneCountInString(omission(count))); kept <= limit; kept++ {
		marker := omission(count - kept)
		size := kept + utf8.RuneCountInString(marker)
		if size == limit {
			head := kept / 3
			return string(runes[:head]) + marker + string(runes[count-(kept-head):])
		}
		if size > limit {
			// Tiny budgets cannot fit the count. Still indicate omission.
			return "…"
		}
	}
	return "…"
}

func omission(n int) string { return fmt.Sprintf("\n…（%d 文字省略）…\n", n) }
