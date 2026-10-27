package guard

import (
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
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
			value := strings.Trim(text[m[2]:m[3]], "\"'")
			if environmentReference.MatchString(value) || m[2] > 0 && text[m[2]-1] == '$' && environmentName.MatchString(value) {
				continue
			}
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

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var environmentReference = regexp.MustCompile(`^\$[A-Za-z_][A-Za-z0-9_]*$`)

// BRIEF-2 criterion 9: restore qualifying values before pattern redaction.
func (i Input) restoreEnvironment(text string) string {
	type binding struct{ name, value string }
	var bindings []binding
	for key, value := range i.Env {
		if utf8.RuneCountInString(value) < 8 {
			continue
		}
		upper := strings.ToUpper(key)
		for _, part := range []string{"TOKEN", "KEY", "SECRET", "PASSWORD", "PASS", "AUTH", "CREDENTIAL", "COOKIE", "SESSION"} {
			if strings.Contains(upper, part) {
				bindings = append(bindings, binding{key, value})
				break
			}
		}
	}
	// Prefer a longer value to its prefix; aliases use a stable name.
	sort.Slice(bindings, func(a, b int) bool {
		if len(bindings[a].value) != len(bindings[b].value) {
			return len(bindings[a].value) > len(bindings[b].value)
		}
		return bindings[a].name < bindings[b].name
	})
	var pairs []string
	for _, binding := range bindings {
		pairs = append(pairs, binding.value, "$"+binding.name)
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

func (i Input) redactedArgv() []string {
	argv := append([]string{i.Command}, i.Args...)
	restored := make([]string, len(argv))
	for n, arg := range argv {
		restored[n] = i.restoreArgumentEnvironment(arg)
	}
	return redactArgv(restored)
}

var secretArgument = regexp.MustCompile(`(?i)\b(?:[A-Za-z_][A-Za-z0-9_]*_(?:TOKEN|KEY|SECRET)|PASSWORD|TOKEN|KEY|SECRET)=`)

func redactArgument(arg string) string {
	if loc := secretArgument.FindStringIndex(arg); loc != nil {
		if environmentReference.MatchString(strings.Trim(arg[loc[1]:], "\"'")) {
			return arg
		}
		return arg[:loc[1]] + "***"
	}
	return arg
}

func redact(text string) string {
	// Decode shell words without running substitutions, so quotes around a whole
	// argument protect the same value as quotes immediately after '='.
	tree, _ := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(text), "redaction")
	var spans []secretSpan
	if tree != nil {
		syntax.Walk(tree, func(node syntax.Node) bool {
			word, ok := node.(*syntax.Word)
			if !ok {
				return true
			}
			value, err := expand.Literal(&expand.Config{NoUnset: true}, word)
			if err == nil && redactArgument(value) != value {
				spans = append(spans, secretSpan{int(word.Pos().Offset()), int(word.End().Offset())})
				return false
			}
			return true
		})
	}
	if len(spans) > 0 {
		sort.Slice(spans, func(a, b int) bool { return spans[a].start < spans[b].start })
		text = maskSpans(text, spans)
	}
	return maskSpans(text, secretSpans(text))
}

type environmentWord struct {
	start, end        int
	literal, restored string
}

// Decode display copies only; command/process substitutions have no executor.
func (i Input) environmentWords(text string) []environmentWord {
	tree, _ := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(text), "environment display")
	var words []environmentWord
	if tree != nil {
		syntax.Walk(tree, func(node syntax.Node) bool {
			word, ok := node.(*syntax.Word)
			if !ok {
				return true
			}
			fields, err := expand.Fields(&expand.Config{NoUnset: true}, word)
			if err != nil || len(fields) != 1 {
				return true
			}
			restored := i.restoreEnvironment(fields[0])
			if restored == fields[0] {
				return true
			}
			literal, _ := expand.Literal(&expand.Config{NoUnset: true}, word)
			words = append(words, environmentWord{int(word.Pos().Offset()), int(word.End().Offset()), literal, restored})
			return false
		})
	}
	return words
}

func (i Input) restoreShellEnvironment(text string) string {
	words := i.environmentWords(text)
	sort.Slice(words, func(a, b int) bool { return words[a].start > words[b].start })
	for _, word := range words {
		text = text[:word.start] + word.restored + text[word.end:]
	}
	return text
}

func (i Input) restoreArgumentEnvironment(text string) string {
	// The policy parser's Literal result can retain unquoted backslashes.
	// Match that representation to the decoded word without changing policy argv.
	words := i.environmentWords(i.RawLine)
	sort.Slice(words, func(a, b int) bool { return len(words[a].literal) > len(words[b].literal) })
	var pairs []string
	for _, word := range words {
		if word.literal != "" {
			pairs = append(pairs, word.literal, word.restored)
		}
	}
	text = strings.NewReplacer(pairs...).Replace(text)
	return i.restoreEnvironment(text)
}

// Argv boundaries are authoritative when shell quoting is no longer available.
func (i Input) redact(text string) string {
	for _, raw := range []string{i.RawLine, i.TriggerCommand, i.Env["IKESU_GUARD_PARENT_COMMAND"]} {
		if raw != "" && strings.Contains(text, raw) {
			text = strings.ReplaceAll(text, raw, i.restoreShellEnvironment(raw))
		}
	}
	text = i.restoreShellEnvironment(text)
	text = i.restoreArgumentEnvironment(text)
	for _, original := range append([]string{i.Command}, i.Args...) {
		safe := redactArgument(i.restoreArgumentEnvironment(original))
		if safe == original {
			continue
		}
		quoted, err := syntax.Quote(original, syntax.LangBash)
		if err == nil {
			text = strings.ReplaceAll(text, quoted, safe)
		}
		text = strings.ReplaceAll(text, original, safe)
	}
	return redact(text)
}

func redactArgv(argv []string) []string {
	safe := make([]string, len(argv))
	for n, arg := range argv {
		safe[n] = redactArgument(arg)
	}
	argv = safe
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

type redactedWriter struct {
	io.Writer
	input Input
}

func (w redactedWriter) Write(data []byte) (int, error) {
	_, err := io.WriteString(w.Writer, w.input.redact(string(data)))
	if err != nil {
		return 0, err
	}
	return len(data), nil
}
