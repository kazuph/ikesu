package snapshot

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Clean removes terminal controls and progress rows while preserving text and
// indentation. Three or more consecutive blank rows become one.
func Clean(lines []string) []string {
	out := make([]string, 0, len(lines))
	blanks := 0
	for _, line := range lines {
		line = strings.TrimRightFunc(cleanText(line), unicode.IsSpace)
		if progressLine(line) {
			continue
		}
		if line == "" {
			blanks++
			continue
		}
		out = appendBlanks(out, blanks)
		blanks = 0
		out = append(out, line)
	}
	return appendBlanks(out, blanks)
}

func cleanText(text string) string {
	return strings.Map(func(r rune) rune {
		if r != '\t' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
			return -1
		}
		return r
	}, ansi.Strip(text))
}

func appendBlanks(out []string, count int) []string {
	if count >= 3 {
		count = 1
	}
	for range count {
		out = append(out, "")
	}
	return out
}

func progressLine(line string) bool {
	line = strings.ToLower(strings.TrimSpace(line))
	if strings.Contains(line, "esc to interrupt") {
		return true
	}
	r, size := utf8.DecodeRuneInString(line)
	if r >= '⠀' && r <= '⣿' {
		rest := strings.TrimLeft(line[size:], " \t")
		for _, word := range []string{"working", "thinking", "waiting", "processing", "generating", "loading", "running"} {
			if strings.HasPrefix(rest, word) && (len(rest) == len(word) || progressDelimiter(rest[len(word):])) {
				return true
			}
		}
		return rest == ""
	}
	return false
}

func progressDelimiter(rest string) bool {
	r, _ := utf8.DecodeRuneInString(rest)
	return strings.ContainsRune(" .…(·", r)
}
