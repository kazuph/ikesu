package screenprompt

import (
	"strings"
	"unicode/utf8"

	"github.com/kazuph/ikesu/internal/api"
)

// Keep the existing threshold for also retaining a full question in body.
// It never limits the heading: the first paragraph is always displayed in full.
const questionBodyThreshold = 80

func questionParagraphs(lines []string, columns int) string {
	var paragraphs []string
	var rows []string
	flush := func() {
		if len(rows) != 0 {
			paragraphs = append(paragraphs, joinWrapped(rows, columns))
			rows = nil
		}
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			flush()
		} else {
			rows = append(rows, line)
		}
	}
	flush()
	return strings.Join(paragraphs, "\n\n")
}

func questionHeading(text string) string {
	paragraph, _, _ := strings.Cut(text, "\n\n")
	return paragraph
}

func presentQuestion(p *Prompt) {
	full := p.Params.Title
	if len([]rune(full)) <= questionBodyThreshold {
		return
	}
	body := full
	if p.Params.Body != nil && (p.Params.QuestionLabel == nil || *p.Params.Body != *p.Params.QuestionLabel) {
		body += "\n\n" + *p.Params.Body
	}
	p.Params.Title = questionHeading(full)
	p.Params.Body = &body
}

// Summarized decisions persist the full question in body. Paragraph breaks
// and frame-only rows must not change the identity used by delivery/reuse.
func questionIdentity(params api.DecisionCreateParams) string {
	text := params.Title
	if params.Title != "" && params.Body != nil && questionHeading(*params.Body) == params.Title && len([]rune(*params.Body)) > questionBodyThreshold {
		text = *params.Body
	}
	lines, _ := cleanNotice(strings.Split(text, "\n"))
	paragraphs := strings.Split(strings.TrimSpace(strings.Join(lines, "\n")), "\n\n")
	var flat strings.Builder
	for i, paragraph := range paragraphs {
		if i > 0 {
			separator := wrappedSeparator(paragraphs[i-1], paragraph, 0)
			first, _ := utf8.DecodeRuneInString(paragraph)
			if cjk(first) {
				separator = ""
			}
			flat.WriteString(separator)
		}
		flat.WriteString(paragraph)
	}
	return stableText(flat.String())
}
