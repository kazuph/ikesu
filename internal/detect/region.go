package detect

import (
	"regexp"
	"strings"
)

// Screen regions: detection rules scope their gates to a slice of the
// pane screen (or to OSC state). Regions return contiguous substrings of
// the original screen text, so they are cheap to compute per rule.

// textLine is one screen line as byte offsets; end excludes the newline.
// A trailing newline never counts as another line.
type textLine struct {
	start, end int
}

// splitLines cuts s into lines; the text after the last newline (empty or
// not) is not counted when empty.
func splitLines(s string) []textLine {
	if s == "" {
		return nil
	}
	var lines []textLine
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, textLine{start, i})
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, textLine{start, len(s)})
	}
	return lines
}

// isHorizontalRule reports whether line is a box border made of ─ runes:
// trimmed, it starts with at least one ─, and what follows is either
// whitespace only or another run of 3+ ─.
func isHorizontalRule(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	i := 0
	for i < len(t) && strings.HasPrefix(t[i:], "─") {
		i += len("─")
	}
	if i == 0 {
		return false
	}
	rest := strings.TrimSpace(t[i:])
	return rest == "" || strings.HasPrefix(rest, "───")
}

// isCodexPromptLine reports Codex's input marker: exactly `›` or a line
// starting `› ` (compared verbatim, no trimming).
func isCodexPromptLine(line string) bool {
	return line == "›" || strings.HasPrefix(line, "› ")
}

// isCodexSepLine reports a Codex block separator drawn at column 0.
func isCodexSepLine(line string) bool {
	return strings.HasPrefix(line, "•") || strings.HasPrefix(line, "■") ||
		strings.HasPrefix(line, "✗") || strings.HasPrefix(line, "✓")
}

var bottomNRe = regexp.MustCompile(`^(bottom_lines|bottom_non_empty_lines)\((\d+)\)$`)

// knownRegion reports whether name selects a supported region.
func knownRegion(name string) bool {
	name = strings.TrimSpace(name)
	switch name {
	case "osc_title", "osc_progress", "whole_recent",
		"after_last_prompt_marker", "before_current_prompt_marker",
		"whole_recent_without_current_prompt_marker",
		"current_prompt_block_marker", "after_current_prompt_block_marker",
		"prompt_box_body", "above_prompt_box",
		"last_non_empty_above_prompt_box", "after_last_horizontal_rule":
		return true
	}
	return bottomNRe.MatchString(name)
}

// regionText extracts the named region from in.
func regionText(in Input, name string) string {
	name = strings.TrimSpace(name)
	if name == "osc_title" {
		return in.OSCTitle
	}
	if name == "osc_progress" {
		return in.OSCProgress
	}
	if m := bottomNRe.FindStringSubmatch(name); m != nil {
		n := atoiOr(m[2], 0)
		if m[1] == "bottom_lines" {
			return bottomLines(in.Screen, n)
		}
		return bottomNonEmptyLines(in.Screen, n)
	}

	screen := in.Screen
	lines := splitLines(screen)
	at := func(i int) string { return screen[lines[i].start:lines[i].end] }
	// span returns lines [from, to) as the contiguous original substring.
	span := func(from, to int) string {
		if from >= to || from < 0 || to > len(lines) {
			return ""
		}
		return screen[lines[from].start:lines[to-1].end]
	}

	var rules []int
	for i := range lines {
		if isHorizontalRule(at(i)) {
			rules = append(rules, i)
		}
	}
	// The prompt box top is the second-to-last horizontal rule.
	topRule := -1
	if len(rules) >= 2 {
		topRule = rules[len(rules)-2]
	}

	lastPrompt := -1
	for i := range lines {
		if isCodexPromptLine(at(i)) {
			lastPrompt = i
		}
	}
	// A separator below the last input line retires it.
	curPrompt := lastPrompt
	for i := lastPrompt + 1; i < len(lines) && curPrompt >= 0; i++ {
		if isCodexSepLine(at(i)) {
			curPrompt = -1
		}
	}
	// The block marker is the separator closest above the current input
	// line.
	blockMarker := -1
	for i := curPrompt - 1; i >= 0; i-- {
		if isCodexSepLine(at(i)) {
			blockMarker = i
			break
		}
	}

	switch name {
	case "whole_recent":
		return screen
	case "after_last_prompt_marker":
		if lastPrompt < 0 {
			return screen
		}
		return span(lastPrompt+1, len(lines))
	case "before_current_prompt_marker":
		if curPrompt < 0 {
			return screen
		}
		return span(0, curPrompt)
	case "whole_recent_without_current_prompt_marker":
		if curPrompt >= 0 {
			return ""
		}
		return screen
	case "current_prompt_block_marker":
		if blockMarker < 0 {
			return ""
		}
		return at(blockMarker)
	case "after_current_prompt_block_marker":
		if blockMarker < 0 {
			return ""
		}
		return span(blockMarker, len(lines))
	case "prompt_box_body":
		if topRule < 0 {
			return ""
		}
		end := len(lines)
		for _, r := range rules {
			if r > topRule {
				end = r
				break
			}
		}
		return span(topRule+1, end)
	case "above_prompt_box":
		if topRule < 0 {
			return screen
		}
		return span(0, topRule)
	case "last_non_empty_above_prompt_box":
		end := len(lines)
		if topRule >= 0 {
			end = topRule
		}
		for i := end - 1; i >= 0; i-- {
			if strings.TrimSpace(at(i)) != "" {
				return at(i)
			}
		}
		return ""
	case "after_last_horizontal_rule":
		if len(rules) == 0 {
			return screen
		}
		return span(rules[len(rules)-1]+1, len(lines))
	}
	return ""
}

// bottomLines returns the last n lines of s (all of s when n exceeds the
// line count; "" for n == 0).
func bottomLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := splitLines(s)
	if n >= len(lines) {
		return s
	}
	return s[lines[len(lines)-n].start:lines[len(lines)-1].end]
}

// bottomNonEmptyLines returns the region from the n-th non-blank line
// (counted from the bottom) to the end, keeping blank lines in between.
// No non-blank line at all yields ""; fewer than n yields the whole text.
func bottomNonEmptyLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := splitLines(s)
	nonEmpty := -1
	count := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(s[lines[i].start:lines[i].end]) == "" {
			continue
		}
		count++
		if count == n {
			nonEmpty = i
			break
		}
	}
	if count == 0 {
		return ""
	}
	if nonEmpty < 0 {
		return s
	}
	return s[lines[nonEmpty].start:lines[len(lines)-1].end]
}

func atoiOr(s string, def int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}
