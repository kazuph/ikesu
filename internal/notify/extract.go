// Package notify extracts agent-notification bodies from pane text,
// following the herdr SPEC G4 rules: composer chrome and status lines are
// dropped, the last agent-response marker (Codex `• `, Claude Code `⏺ `)
// starts the body, and the result is capped at 120 Unicode scalar values.
package notify

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// sourceLines is how many recent-unwrapped lines feed the extractor.
	sourceLines = 80
	// maxRunes is the body cap in Unicode scalar values.
	maxRunes = 120
)

// spinnerChars lead Claude Code spinner/summary chrome lines.
const spinnerChars = "✻✽✶✢✳✣✤❋✺"

// chromePrefixes lead lines that are composer/statusline chrome.
var chromePrefixes = []string{"gpt-", "›", "❯", ">", "⎿", "└", "⏵⏵", "※"}

// chromeSubstrings mark chrome anywhere in the line.
var chromeSubstrings = []string{
	"worked for ", "% left", "esc to interrupt", "ctrl+c to interrupt",
	"ctrl+o to expand", "? for shortcuts", "/ps to view", "tokens used",
	"disable recaps in /config",
}

// responseMarkers start an agent response block.
var responseMarkers = []string{"• ", "⏺ "}

// Body extracts the notification body from pane lines (recent text,
// screen-bottom last). title lets chrome matching the notification title
// be dropped. Returns "" when only chrome remains.
func Body(lines []string, title string) string {
	if len(lines) > sourceLines {
		lines = lines[len(lines)-sourceLines:]
	}
	clean := make([]string, 0, len(lines))
	for _, l := range lines {
		clean = append(clean, normalize(l))
	}
	// Composer prompt lines (❯ › >) cut off the input/statusline chrome:
	// everything from the first such line onward is discarded.
	for i, l := range clean {
		if isPromptLine(l) {
			clean = clean[:i]
			break
		}
	}
	if body := lastMarkerBody(clean); body != "" {
		return capRunes(body)
	}
	if body := lastPlainBlock(clean, title); body != "" {
		return capRunes(body)
	}
	return ""
}

// normalize collapses whitespace and strips box-drawing edges.
func normalize(l string) string {
	l = strings.TrimFunc(l, isBoxRune)
	l = strings.Join(strings.Fields(l), " ")
	return strings.TrimFunc(l, isBoxRune)
}

func isBoxRune(r rune) bool {
	// Box Drawing (2500–257F), Block Elements (2580–259F).
	return r >= 0x2500 && r <= 0x259F
}

// isPromptLine reports whether l is a composer prompt (❯, ›, > alone or
// leading the line).
func isPromptLine(l string) bool {
	return strings.HasPrefix(l, "❯") || strings.HasPrefix(l, "›") || strings.HasPrefix(l, ">")
}

func isChrome(l, title string) bool {
	if l == "" {
		return true
	}
	if title != "" && l == title {
		return true
	}
	for _, p := range chromePrefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	if r, _ := utf8.DecodeRuneInString(l); strings.ContainsRune(spinnerChars, r) {
		return true
	}
	for _, sub := range chromeSubstrings {
		if strings.Contains(l, sub) {
			return true
		}
	}
	// "<n> task(s) (" task summaries.
	if i := strings.Index(l, " task(s) ("); i > 0 {
		if _, err := strconv.Atoi(strings.TrimSpace(l[:i])); err == nil {
			return true
		}
	}
	return false
}

func hasMarkerPrefix(l string) (rest string, ok bool) {
	for _, m := range responseMarkers {
		if strings.HasPrefix(l, m) {
			return strings.TrimSpace(l[len(m):]), true
		}
	}
	return "", false
}

// lastMarkerBody finds the last agent-response marker and joins the
// marker's text with following lines until the next marker or chrome.
func lastMarkerBody(lines []string) string {
	idx := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if _, ok := hasMarkerPrefix(lines[i]); ok {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ""
	}
	var parts []string
	for i := idx; i < len(lines); i++ {
		l := lines[i]
		if i > idx {
			if _, ok := hasMarkerPrefix(l); ok {
				break
			}
		}
		rest, isMarker := hasMarkerPrefix(l)
		if isMarker {
			l = rest
		} else if isChrome(l, "") {
			break
		}
		if l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, " ")
}

// lastPlainBlock takes the first non-chrome line of the last non-empty
// block when no response marker exists.
func lastPlainBlock(lines []string, title string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		if l == "" {
			continue
		}
		if isChrome(l, title) {
			continue
		}
		return l
	}
	return ""
}

// capRunes truncates to maxRunes scalar values (119 + '…' when over).
func capRunes(s string) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes-1]) + "…"
}
