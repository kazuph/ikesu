package guard

import (
	"io"
	"regexp"
	"sort"
	"strings"
)

// Group 1 is the secret. Matching only affects display and persisted records.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Authorization\s*:\s*Bearer\s+["']?([^\s"']+)`),
	regexp.MustCompile(`\b((?:sk-|xox[bp]-|ghp_|github_pat_|AKIA)[A-Za-z0-9_-]+)`),
	regexp.MustCompile(`(?i)\b(?:[A-Za-z_][A-Za-z0-9_]*_(?:TOKEN|KEY|SECRET)|PASSWORD|TOKEN|KEY|SECRET)=("[^"]*"|'[^']*'|[^\s;"']+)`),
	regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://([^/\s@]+:[^/\s@]+)@`),
}

type secretSpan struct{ start, end int }

func secretSpans(text string) []secretSpan {
	var spans []secretSpan
	for _, pattern := range secretPatterns {
		for _, m := range pattern.FindAllStringSubmatchIndex(text, -1) {
			spans = append(spans, secretSpan{m[2], m[3]})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var merged []secretSpan
	for _, span := range spans {
		if len(merged) > 0 && span.start <= merged[len(merged)-1].end {
			if span.end > merged[len(merged)-1].end {
				merged[len(merged)-1].end = span.end
			}
		} else {
			merged = append(merged, span)
		}
	}
	return merged
}
func maskSpans(text string, spans []secretSpan) string {
	var out strings.Builder
	at := 0
	for _, span := range spans {
		out.WriteString(text[at:span.start])
		out.WriteString("***")
		at = span.end
	}
	out.WriteString(text[at:])
	return out.String()
}
func redact(text string) string { return maskSpans(text, secretSpans(text)) }
func redactArgv(argv []string) []string {
	spans := secretSpans(strings.Join(argv, " "))
	out := make([]string, len(argv))
	offset := 0
	for n, arg := range argv {
		var local []secretSpan
		for _, span := range spans {
			start, end := max(span.start-offset, 0), min(span.end-offset, len(arg))
			if start < end {
				local = append(local, secretSpan{start, end})
			}
		}
		out[n] = maskSpans(arg, local)
		offset += len(arg) + 1
	}
	return out
}

type redactedWriter struct{ io.Writer }

func (w redactedWriter) Write(data []byte) (int, error) {
	_, err := io.WriteString(w.Writer, redact(string(data)))
	if err != nil {
		return 0, err
	}
	return len(data), nil
}
