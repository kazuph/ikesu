package detect

import (
	"strconv"
	"strings"
)

// regionText extracts the named region from the detection input. Unknown
// region names return "" (they are rejected at manifest load anyway).
func regionText(in Input, spec string) string {
	name := strings.TrimSpace(spec)
	switch name {
	case "osc_title":
		return in.OSCTitle
	case "osc_progress":
		return in.OSCProgress
	}
	content := in.Screen
	switch name {
	case "whole_recent":
		return content
	case "after_last_prompt_marker":
		return afterLastPromptMarker(content)
	case "before_current_prompt_marker":
		return beforeCurrentPromptMarker(content)
	case "whole_recent_without_current_prompt_marker":
		if currentCodexPromptIndex(lines(content)) >= 0 {
			return ""
		}
		return content
	case "current_prompt_block_marker":
		return currentPromptBlockMarker(content)
	case "after_current_prompt_block_marker":
		return afterCurrentPromptBlockMarker(content)
	case "prompt_box_body":
		return promptBoxBody(content)
	case "above_prompt_box":
		return abovePromptBox(content)
	case "last_non_empty_above_prompt_box":
		return lastNonEmptyLine(abovePromptBox(content))
	case "after_last_horizontal_rule":
		return afterLastHorizontalRule(content)
	}
	if n, ok := regionCount(name, "bottom_lines"); ok {
		return bottomLines(content, n)
	}
	if n, ok := regionCount(name, "bottom_non_empty_lines"); ok {
		return bottomNonEmptyLines(content, n)
	}
	return ""
}

// knownRegion reports whether spec names a region we implement.
func knownRegion(spec string) bool {
	name := strings.TrimSpace(spec)
	switch name {
	case "whole_recent", "osc_title", "osc_progress",
		"after_last_prompt_marker", "before_current_prompt_marker",
		"whole_recent_without_current_prompt_marker",
		"current_prompt_block_marker", "after_current_prompt_block_marker",
		"prompt_box_body", "above_prompt_box",
		"last_non_empty_above_prompt_box", "after_last_horizontal_rule":
		return true
	}
	_, a := regionCount(name, "bottom_lines")
	_, b := regionCount(name, "bottom_non_empty_lines")
	return a || b
}

func regionCount(spec, name string) (int, bool) {
	rest, ok := strings.CutPrefix(spec, name+"(")
	if !ok || !strings.HasSuffix(rest, ")") {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:len(rest)-1])
	return n, err == nil
}

// lines splits like Rust's str::lines(): "" yields nothing and a trailing
// newline does not produce a phantom empty last element.
func lines(content string) []string {
	if content == "" {
		return nil
	}
	ls := strings.Split(content, "\n")
	if ls[len(ls)-1] == "" {
		ls = ls[:len(ls)-1]
	}
	return ls
}

func bottomLines(content string, count int) string {
	ls := lines(content)
	start := len(ls) - count
	if start < 0 {
		start = 0
	}
	return strings.Join(ls[start:], "\n")
}

func bottomNonEmptyLines(content string, count int) string {
	ls := lines(content)
	seen := 0
	start := -1
	for i := len(ls) - 1; i >= 0; i-- {
		if strings.TrimSpace(ls[i]) != "" {
			seen++
			start = i
			if seen == count {
				break
			}
		}
	}
	if start < 0 {
		return ""
	}
	return strings.Join(ls[start:], "\n")
}

func codexPromptLine(line string) bool {
	return line == "›" || strings.HasPrefix(line, "› ")
}

func codexBlockMarkerLine(line string) bool {
	return strings.HasPrefix(line, "•") || strings.HasPrefix(line, "■") ||
		strings.HasPrefix(line, "✗") || strings.HasPrefix(line, "✓")
}

func afterLastPromptMarker(content string) string {
	ls := lines(content)
	idx := -1
	for i := len(ls) - 1; i >= 0; i-- {
		if codexPromptLine(ls[i]) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return content
	}
	return strings.Join(ls[idx+1:], "\n")
}

// currentCodexPromptIndex is the last "›" prompt with no block marker
// below it.
func currentCodexPromptIndex(ls []string) int {
	idx := -1
	for i := len(ls) - 1; i >= 0; i-- {
		if codexPromptLine(ls[i]) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return -1
	}
	for _, l := range ls[idx+1:] {
		if codexBlockMarkerLine(l) {
			return -1
		}
	}
	return idx
}

func beforeCurrentPromptMarker(content string) string {
	ls := lines(content)
	idx := currentCodexPromptIndex(ls)
	if idx < 0 {
		return content
	}
	return strings.Join(ls[:idx], "\n")
}

func currentPromptBlockMarker(content string) string {
	ls := lines(content)
	idx := currentCodexPromptIndex(ls)
	if idx < 0 {
		return ""
	}
	for i := idx - 1; i >= 0; i-- {
		if codexBlockMarkerLine(ls[i]) {
			return ls[i]
		}
	}
	return ""
}

func afterCurrentPromptBlockMarker(content string) string {
	ls := lines(content)
	idx := currentCodexPromptIndex(ls)
	if idx < 0 {
		return ""
	}
	for i := idx - 1; i >= 0; i-- {
		if codexBlockMarkerLine(ls[i]) {
			return strings.Join(ls[i:], "\n")
		}
	}
	return ""
}

// isHorizontalRule: trimmed line of ≥3 "─" chars, optionally followed by
// text (rules may carry a title inside them).
func isHorizontalRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	n := 0
	for _, ch := range trimmed {
		if ch != '─' {
			break
		}
		n++
	}
	if n == 0 {
		return false
	}
	suffix := strings.TrimLeft(trimmed[len("─")*n:], " \t")
	return suffix == "" || n >= 3
}

// promptBoxBody is the text between the last prompt box's top border and
// its closing border. The top border is the second-to-last horizontal rule.
func promptBoxBody(content string) string {
	ls := lines(content)
	top := promptBoxTopBorderIndex(ls)
	if top < 0 {
		return ""
	}
	end := len(ls)
	for i := top + 1; i < len(ls); i++ {
		if isHorizontalRule(ls[i]) {
			end = i
			break
		}
	}
	if top+1 >= end {
		return ""
	}
	return strings.Join(ls[top+1:end], "\n")
}

func abovePromptBox(content string) string {
	ls := lines(content)
	top := promptBoxTopBorderIndex(ls)
	if top < 0 {
		return content
	}
	return strings.Join(ls[:top], "\n")
}

func promptBoxTopBorderIndex(ls []string) int {
	count := 0
	for i := len(ls) - 1; i >= 0; i-- {
		if isHorizontalRule(ls[i]) {
			count++
			if count == 2 {
				return i
			}
		}
	}
	return -1
}

func afterLastHorizontalRule(content string) string {
	ls := lines(content)
	last := -1
	for i, l := range ls {
		if isHorizontalRule(l) {
			last = i
		}
	}
	if last < 0 {
		return content
	}
	return strings.Join(ls[last+1:], "\n")
}

func lastNonEmptyLine(content string) string {
	ls := lines(content)
	for i := len(ls) - 1; i >= 0; i-- {
		if strings.TrimSpace(ls[i]) != "" {
			return ls[i]
		}
	}
	return ""
}
