package snapshot

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const DefaultLines = 120
const MaxLines = 5000

// Go does not have constant slices.
var promptPlaceholders = []string{
	"ask codex to do anything",
	"ask anything",
	"ask a question",
	"type your message",
	"type a message",
	"type your message or @path/to/file",
	"? for shortcuts",
}

var shellPrompt = regexp.MustCompile(`^(?:\[[^]]+\]|[^[:space:]\p{Zs}]+)?[\t\p{Zs}]*[$%#❯][\t\p{Zs}]+(.+)$`)

// LastTurn returns the latest non-empty prompt and everything after it.
func LastTurn(lines []string, agent string, fallback int) (turn []string, found bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if isPromptLine(lines[i], agent) {
			return lines[i:], true
		}
	}
	if fallback <= 0 {
		fallback = DefaultLines
	}
	return lines[max(0, len(lines)-fallback):], false
}

func isPromptLine(line, agent string) bool {
	line = strings.TrimSpace(line)
	var input string
	if agent == "" {
		match := shellPrompt.FindStringSubmatch(line)
		if match == nil {
			return false
		}
		input = match[1]
	} else {
		marker, size := utf8.DecodeRuneInString(line)
		allowed := false
		switch agent {
		case "codex":
			allowed = marker == '›'
		case "claude":
			allowed = marker == '>' || marker == '❯'
		case "agy", "gemini":
			allowed = marker == '>'
		}
		space, _ := utf8.DecodeRuneInString(line[size:])
		if !allowed || !unicode.IsSpace(space) {
			return false
		}
		input = line[size:]
	}
	input = strings.ToLower(strings.TrimSpace(input))
	for _, placeholder := range promptPlaceholders {
		if strings.TrimRight(input, " .…") == placeholder {
			return false
		}
	}
	return input != ""
}
