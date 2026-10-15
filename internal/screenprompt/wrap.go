package screenprompt

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func screenColumns(lines []string) int {
	width := 0
	for _, line := range lines {
		width = max(width, ansi.StringWidth(line))
	}
	return width
}

func cjk(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// Claude wraps prose at word boundaries, but splits CJK and oversized tokens
// at the screen edge. Keep the physical previous row when deciding that edge.
func wrappedSeparator(previous, next string, columns int) string {
	left, right := strings.TrimSpace(previous), strings.TrimSpace(next)
	if left == "" || right == "" {
		return " "
	}
	a, _ := utf8.DecodeLastRuneInString(left)
	b, _ := utf8.DecodeRuneInString(right)
	if (cjk(a) || a > unicode.MaxASCII && unicode.IsPunct(a)) && (cjk(b) || b > unicode.MaxASCII && unicode.IsPunct(b)) {
		return ""
	}
	fragment := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("/\\_.-", r)
	}
	if !fragment(a) || !fragment(b) {
		return " "
	}
	last := left[strings.LastIndexFunc(left, unicode.IsSpace)+1:]
	first := right
	if i := strings.IndexFunc(first, unicode.IsSpace); i >= 0 {
		first = first[:i]
	}
	if strings.ContainsAny(last+first, "/\\_-") || strings.Contains(strings.TrimRight(last, "."), ".") || strings.Contains(strings.TrimRight(first, ".,;:!?"), ".") || strings.HasSuffix(last, ".") && unicode.IsLower(b) {
		return ""
	}
	if columns > 0 && ansi.StringWidth(last+first) > columns {
		return ""
	}
	if columns > 0 && ansi.StringWidth(previous) >= columns-1 && strings.ContainsFunc(previous, cjk) {
		return ""
	}
	return " "
}

func joinWrapped(lines []string, columns int) string {
	var out strings.Builder
	for i, line := range lines {
		if i > 0 {
			out.WriteString(wrappedSeparator(lines[i-1], line, columns))
		}
		out.WriteString(strings.TrimSpace(line))
	}
	return out.String()
}
