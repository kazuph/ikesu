package screenprompt

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func screenColumns(lines []string) int {
	width, ruleWidth := 0, 0
	for _, line := range lines {
		width = max(width, ansi.StringWidth(line))
		if solidRule(line) {
			ruleWidth = max(ruleWidth, ansi.StringWidth(line))
		}
	}
	if ruleWidth > 0 {
		return ruleWidth
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
	tokenBreak := func(r rune) bool { return unicode.IsSpace(r) || cjk(r) || r > unicode.MaxASCII && unicode.IsPunct(r) }
	last := left
	if i := strings.LastIndexFunc(left, tokenBreak); i >= 0 {
		_, size := utf8.DecodeRuneInString(left[i:])
		last = left[i+size:]
	}
	first := right
	if i := strings.IndexFunc(first, tokenBreak); i >= 0 {
		first = first[:i]
	}
	base := last[strings.LastIndexAny(last, "/\\")+1:]
	pathPrefix := strings.ContainsAny(last, "/\\_-") && !strings.Contains(base, ".")
	pathSuffix := strings.ContainsAny(first, "/\\_-") || strings.Contains(strings.TrimRight(first, ".,;:!?"), ".")
	if pathPrefix && pathSuffix || strings.ContainsRune("/\\_-", a) || strings.ContainsRune("/\\_.-", b) {
		return ""
	}
	indent := ansi.StringWidth(previous) - ansi.StringWidth(strings.TrimLeftFunc(previous, unicode.IsSpace))
	if columns > indent && ansi.StringWidth(last+first) > columns-indent {
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
