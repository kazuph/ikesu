package guard

import (
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
	"strings"
)

func containsReasonName(i Input, text string) bool {
	for _, name := range []string{"AI_GUARD_REQUEST_REASON", "AI_GUARD_REQUEST_ENV_NAME", "IKESU_GUARD_PARENT_REASON", requestReasonEnvName(i)} {
		if strings.Contains(text, name) {
			return true
		}
	}
	return false
}

func displayDeclaration(words []string) bool {
	if len(words) < 2 || !has([]string{"declare", "typeset", "export"}, words[0]) {
		return false
	}
	printed := false
	for _, word := range words[1:] {
		if word == "-p" {
			printed = true
			continue
		}
		if word == "--" {
			continue
		}
		if strings.HasPrefix(word, "-") || strings.Contains(word, "=") {
			return false
		}
	}
	return printed
}

func runtimeReasonSyntax(i Input, node syntax.Node, cfg *expand.Config) (bool, bool) {
	var words []*syntax.Word
	var command string
	switch n := node.(type) {
	case *syntax.LetClause:
		var raw strings.Builder
		_ = syntax.NewPrinter().Print(&raw, n)
		return containsReasonName(i, raw.String()) || strings.ContainsAny(raw.String(), "$`"), false
	case *syntax.Assign:
		return n.Name != nil && containsReasonName(i, n.Name.Value), false
	case *syntax.DeclClause:
		command = n.Variant.Value
		for _, arg := range n.Args {
			if arg.Name != nil {
				continue
			}
			if arg.Value != nil {
				words = append(words, arg.Value)
			}
		}
	case *syntax.CallExpr:
		words = n.Args
	default:
		return false, false
	}
	raw := make([]string, 0, len(words))
	expanded := make([]string, 0, len(words))
	if command != "" {
		raw = append(raw, command)
		expanded = append(expanded, command)
	}
	uncertain := false
	for _, word := range words {
		var text strings.Builder
		_ = syntax.NewPrinter().Print(&text, word)
		value, err := shellLiteral(cfg, word)
		raw = append(raw, text.String())
		expanded = append(expanded, value)
		uncertain = uncertain || err != nil || strings.ContainsAny(text.String(), "$`")
	}
	// Assign.Name entries must also be checked before granting the display exception.
	if n, ok := node.(*syntax.DeclClause); ok {
		for _, arg := range n.Args {
			if arg.Name != nil {
				value := arg.Name.Value
				if !arg.Naked || arg.Value != nil || arg.Array != nil {
					value += "="
				}
				raw = append(raw, value)
				expanded = append(expanded, value)
			}
		}
	}
	displaySafe := true
	if n, ok := node.(*syntax.CallExpr); ok && len(n.Assigns) > 0 {
		displaySafe = false
	}
	if displaySafe && !uncertain && displayDeclaration(expanded) {
		return false, true
	}
	for len(expanded) > 0 && has([]string{"command", "builtin", "exec"}, expanded[0]) {
		expanded = expanded[1:]
		for len(expanded) > 0 && strings.HasPrefix(expanded[0], "-") {
			expanded = expanded[1:]
		}
	}
	if len(expanded) == 0 {
		return false, false
	}
	cmd := expanded[0]
	if has([]string{"declare", "local", "typeset"}, cmd) {
		for _, flag := range expanded[1:] {
			if strings.HasPrefix(flag, "-") && strings.Contains(flag, "n") {
				return true, false
			}
		}
	}
	dynamicWrite := has([]string{"read", "readarray", "mapfile", "let", "export", "declare", "local", "typeset", "unset", "unsetenv", "setenv"}, cmd)
	if cmd == "printf" {
		if len(raw) > 1 && len(expanded) > 1 && expanded[1] == "" && strings.ContainsAny(raw[1], "$`") {
			dynamicWrite = true
		}
		for _, flag := range expanded[1:] {
			if flag == "-v" {
				dynamicWrite = true
				break
			}
		}
	}
	if dynamicWrite || cmd == "env" {
		for _, word := range append(raw, expanded...) {
			if containsReasonName(i, word) {
				return true, false
			}
		}
	}
	return dynamicWrite && uncertain, false
}
