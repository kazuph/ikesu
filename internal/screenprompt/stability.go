package screenprompt

import (
	"github.com/kazuph/goterrace/internal/api"
	"regexp"
	"strings"
)

var countdown = regexp.MustCompile(`\bin \d+:\d{2}\b`)
var elapsed = regexp.MustCompile(`\(\d+s\)`)
var spinner = regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]`)
var denial = regexp.MustCompile(`(?i)⚠?\s*Claude\s+Code\s+will\s+automatically\s+deny\s+this\s+request(?:\s+in\s+\d+:\d{2})?,?\s+to\s+avoid\s+blocking\s+progress\s+on\s+an\s+unattended\s+session`)

func stableText(text string) string {
	text = countdown.ReplaceAllString(text, "in M:SS")
	text = elapsed.ReplaceAllString(text, "(Ns)")
	text = spinner.ReplaceAllString(text, "")
	return strings.Join(strings.Fields(text), " ")
}

func cleanNotice(lines []string) ([]string, string) {
	// Replace the complete warning across wrapped physical lines; preserve unrelated line boundaries.
	text := strings.Join(lines, "\n")
	notice := ""
	text = denial.ReplaceAllStringFunc(text, func(value string) string {
		notice = countdown.ReplaceAllString(strings.Join(strings.Fields(value), " "), "")
		notice = strings.Join(strings.Fields(notice), " ")
		return "\n"
	})
	return strings.Split(text, "\n"), notice
}

func positionalTextIDs(lines []string) (string, string) {
	for i, line := range lines {
		if !solidRule(line) {
			continue
		}
		var below []string
		for _, next := range lines[i+1:] {
			if m := numbered.FindStringSubmatch(next); m != nil {
				below = append(below, m[2])
			}
		}
		if len(below) != 1 {
			continue
		}
		// The one row below the separator is the chat/free row even after its text changes.
		last := ""
		for _, prev := range lines[:i] {
			if m := numbered.FindStringSubmatch(prev); m != nil {
				last = m[2]
			}
		}
		if last != "" {
			return last, below[0]
		}
	}
	return "", ""
}

func flowTabs(line string) (string, int, int) {
	var names []string
	current := -1
	for _, m := range tabs.FindAllStringSubmatch(line, -1) {
		name := strings.TrimSpace(m[2])
		if name == "Submit" {
			continue
		}
		if current < 0 && m[1] == "☐" {
			current = len(names)
		}
		names = append(names, name)
	}
	return strings.Join(names, "\x00"), current, len(names)
}

func finalPrompt(p *Prompt) bool {
	if len(p.Params.Options) == 0 || p.Params.AllowText {
		return false
	}
	submit := false
	for _, o := range p.Params.Options {
		label := strings.ToLower(o.Label)
		if strings.HasPrefix(label, "submit") {
			submit = true
			continue
		}
		if label != "cancel" {
			return false
		}
	}
	title := strings.ToLower(p.Params.Title)
	return submit && (p.Flow != "" && p.Question < 0 || strings.Contains(title, "submit") || strings.Contains(title, "回答を送信"))
}

func (p *Prompt) PreviewSignature() string {
	parts := []string{stableText(p.Params.Title)}
	for _, o := range p.Params.Options {
		parts = append(parts, o.ID, stableText(o.Label))
	}
	return strings.Join(parts, "\x00")
}

func (p *Prompt) ChoiceSignature() string {
	q := *p
	q.Params.Title = ""
	q.Params.Options = append([]api.DecisionOption{}, p.Params.Options...)
	for i := range q.Params.Options {
		q.Params.Options[i].Description = nil
	}
	return q.Signature()
}

// Claude's permission command wraps inside left-bordered rows, separate from preview boxes.
func mergeCommandRows(lines []string) []string {
	var out []string
	command := ""
	flush := func() {
		if command != "" {
			out = append(out, command)
			command = ""
		}
	}
	for _, line := range lines {
		trim := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trim, "│ ") {
			command += strings.TrimPrefix(trim, "│ ")
		} else {
			flush()
			out = append(out, line)
		}
	}
	flush()
	return out
}
