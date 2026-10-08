package detect

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Activity fingerprints are the live "working" evidence on a pane screen:
// the spinner/hint lines an agent keeps redrawing while it works. They
// feed SPEC G3's fossil expiry — an unchanged fingerprint for 15 s means
// the lines are leftovers, not live work.

// claudeSpinnerChars are the glyphs Claude Code draws as its spinner.
const claudeSpinnerChars = "·✱✲✳✴✵✶✷✸✹✺✻✼✽✾✿❀❁❂❃❇❈❉❊❋✢✣✤✥✦✧✨⊛⊕⊙◉◎◍⁂⁕※⍟☼★☆"

// isClaudeSpinnerLine reports a live Claude spinner line: a spinner glyph,
// then whitespace, then text containing `…` and at least one letter or
// digit. A finished summary line (✻ Cooked for …) lacks the ellipsis.
func isClaudeSpinnerLine(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	r, size := utf8.DecodeRuneInString(t)
	if !strings.ContainsRune(claudeSpinnerChars, r) {
		return false
	}
	rest := t[size:]
	if rest == "" {
		return false
	}
	next, _ := utf8.DecodeRuneInString(rest)
	if !unicode.IsSpace(next) {
		return false
	}
	body := strings.TrimSpace(rest)
	if !strings.ContainsRune(body, '…') {
		return false
	}
	for _, c := range body {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			return true
		}
	}
	return false
}

// isClaudeActivityLine reports Claude's live work evidence: a spinner line
// or an interrupt hint.
func isClaudeActivityLine(line string) bool {
	if isClaudeSpinnerLine(line) {
		return true
	}
	l := strings.ToLower(line)
	return strings.Contains(l, "esc to interrupt") ||
		strings.Contains(l, "ctrl+c to interrupt")
}

// isCodexActivityLine reports Codex's live work evidence: a `• Working (`
// status line (leading whitespace ignored) or an interrupt hint.
func isCodexActivityLine(line string) bool {
	t := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(t, "•") && strings.Contains(t, "Working (") {
		return true
	}
	return strings.Contains(strings.ToLower(line), "esc to interrupt")
}

// ActivityFingerprint returns the agent's work-evidence lines joined by
// newlines, or "" when the screen shows none.
func ActivityFingerprint(agent, content string) string {
	switch agent {
	case "claude":
		var out []string
		region := regionText(Input{Screen: content}, "above_prompt_box")
		for _, l := range splitLines(region) {
			line := region[l.start:l.end]
			if isClaudeActivityLine(line) {
				out = append(out, line)
			}
		}
		return strings.Join(out, "\n")
	case "codex":
		return codexActivityFingerprint(content)
	}
	return ""
}

// codexActivityFingerprint collects Codex's live lines inside its "live
// region": the block of empty/work lines directly above the most recent
// `›` prompt, or the prompt itself when nothing alive precedes it.
func codexActivityFingerprint(content string) string {
	lines := splitLines(content)
	at := func(i int) string { return content[lines[i].start:lines[i].end] }

	last := -1
	for i := range lines {
		if strings.HasPrefix(strings.TrimLeft(at(i), " \t"), "›") {
			last = i
		}
	}
	top := 0
	if last >= 0 {
		top = last
		sawWork := false
		for i := last - 1; i >= 0; i-- {
			line := at(i)
			if strings.TrimSpace(line) != "" && !isCodexActivityLine(line) {
				break
			}
			if isCodexActivityLine(line) {
				sawWork = true
			}
			top = i
		}
		if !sawWork {
			top = last
		}
	}
	var out []string
	for i := top; i < len(lines); i++ {
		if isCodexActivityLine(at(i)) {
			out = append(out, at(i))
		}
	}
	return strings.Join(out, "\n")
}
