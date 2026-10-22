package screenprompt

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func navigationFooter(line string) bool {
	return strings.Contains(line, "↑/↓") && strings.Contains(strings.ToLower(line), "esc to cancel")
}

// Keep history outside the active question out of its title and preview.
func currentQuestionLines(lines []string) []string {
	first := -1
	for i, line := range lines {
		if m := numbered.FindStringSubmatch(line); m != nil && m[2] == "1" {
			first = i
		}
	}
	if first < 0 {
		return lines
	}
	start := 0
	for i, line := range lines[:first] {
		if navigationFooter(line) {
			start = i + 1
		}
	}
	return lines[start:]
}

// A preview belongs to the options immediately above the last navigation
// footer, and its left border must be to the right of those options.
func extractPreview(lines []string) ([]string, *string, bool) {
	footer := -1
	for i, line := range lines {
		if navigationFooter(line) {
			footer = i
		}
	}
	if footer < 0 {
		return lines, nil, false
	}
	first := -1
	for i, line := range lines[:footer] {
		if m := numbered.FindStringSubmatch(line); m != nil && m[2] == "1" {
			first = i
		}
	}
	if first < 0 {
		return lines, nil, false
	}
	start, column := -1, -1
	for i := first; i < footer; i++ {
		line := lines[i]
		at := strings.IndexAny(line, "┌╭")
		if at < 0 {
			continue
		}
		prefix := strings.TrimRight(line[:at], " ")
		if !numbered.MatchString(prefix) {
			continue
		}
		if !strings.ContainsAny(line[at:], "┐╮") {
			continue
		}
		start, column = i, ansi.StringWidth(line[:at])
		break
	}
	if start < 0 {
		return lines, nil, false
	}
	plain := append([]string{}, lines[:start]...)
	var content []string
	closed := false
	for i := start; i < footer; i++ {
		width := ansi.StringWidth(lines[i])
		if width <= column {
			return lines, nil, true
		}
		prefix := strings.TrimRight(ansi.Cut(lines[i], 0, column), " ")
		if prefix != "" {
			plain = append(plain, prefix)
		}
		box := ansi.Cut(lines[i], column, width)
		if i == start {
			continue
		}
		if strings.HasPrefix(box, "└") || strings.HasPrefix(box, "╰") {
			plain = append(plain, lines[i+1:]...)
			closed = true
			break
		}
		if !strings.HasPrefix(box, "│") {
			return lines, nil, true
		}
		value := strings.TrimPrefix(box, "│")
		end := strings.LastIndex(value, "│")
		if end < 0 {
			return lines, nil, true
		}
		content = append(content, strings.TrimRight(value[:end], " "))
	}
	if !closed {
		return lines, nil, true
	}
	value := strings.Join(content, "\n")
	if strings.TrimSpace(value) == "No preview available" {
		value = ""
	}
	return plain, &value, true
}
