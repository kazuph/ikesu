package screenprompt

import (
	"regexp"
	"strings"
)

var codexQuestionHeader = regexp.MustCompile(`^Question [0-9]+/[0-9]+ \([0-9]+ unanswered\)$`)
var codexTime = regexp.MustCompile(`[0-9]+(?::[0-9]+)*(?:\.[0-9]+)?`)

// The header is the boundary between the transcript and the active question.
func codexQuestionScreen(lines []string) ([]string, string, string) {
	start, label, headerBody := -1, "", ""
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		header, remaining, timed := strings.Cut(trim, " · auto-resolves in ")
		if codexQuestionHeader.MatchString(header) {
			start, label, headerBody = i, header, ""
			if timed {
				headerBody = "auto-resolves in " + remaining
			}
		}
	}
	if start < 0 {
		return lines, "", ""
	}
	var out []string
	var body []string
	if headerBody != "" {
		body = append(body, codexCountdownBody(headerBody))
	}
	for _, line := range lines[start+1:] {
		if strings.Contains(strings.ToLower(line), "auto-resolves in") {
			body = append(body, codexCountdownBody(line))
			continue
		}
		out = append(out, line)
	}
	return out, label, strings.Join(body, "\n")
}

func codexCountdownBody(line string) string {
	return strings.Join(strings.Fields(codexTime.ReplaceAllString(strings.TrimSpace(line), "")), " ")
}

func codexQuestionComplete(lines []string) bool {
	for _, line := range lines {
		lower := strings.ToLower(line)
		if (strings.Contains(lower, "enter to submit answer") || strings.Contains(lower, "enter to submit all")) && (strings.Contains(lower, "tab to add notes") || strings.Contains(lower, "tab or esc to clear notes")) {
			return true
		}
	}
	return false
}

func codexNotesOpen(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), "tab or esc to clear notes") {
			return true
		}
	}
	return false
}
