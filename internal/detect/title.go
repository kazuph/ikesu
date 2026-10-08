package detect

import "strings"

// StripTerminalTitle follows the distribution's observed OSC title contract:
// remove one standalone spinner token, preserving ordinary glyphs and repeated tokens.
func StripTerminalTitle(raw string) string {
	s := strings.TrimSpace(raw)
	first, rest, found := strings.Cut(s, " ")
	if len([]rune(first)) == 1 && strings.ContainsRune("·✳✶✻✽✢⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏", []rune(first)[0]) {
		if !found {
			return ""
		}
		return strings.TrimSpace(rest)
	}
	return s
}
