package guard

import (
	"bytes"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

const claudeBootstrap = `source IKESU_SNAPSHOT 2>/dev/null || true && { shopt -u extglob || setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL; } >/dev/null 2>&1 || true && { \builtin unalias -- 'unsetenv'; \builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && eval IKESU_PAYLOAD < /dev/null && pwd -P >| IKESU_CWD`
const claudeGitBody = "if ! 'typeset' -f 'git' >/dev/null 2>&1 && ! 'alias' 'git' >/dev/null 2>&1 && ( 'eval' 'function git { '\\''local'\\'' _cc_skip= _cc_word; for _cc_word in ${1+\"$@\"}; do if [ -n \"$_cc_skip\" ]; then _cc_skip=; continue; fi; case \"$_cc_word\" in -C|-c|--attr-source|--shallow-file|--git-dir|--separate-git-dir|--work-tree|--namespace|--super-prefix|--config-env) _cc_skip=1 ;; -*) ;; grep) '\\''/usr/bin/env'\\'' LC_ALL=C '\\''git'\\'' ${1+\"$@\"}; '\\''return'\\'' ;; *) break ;; esac; done; '\\''command'\\'' '\\''git'\\'' ${1+\"$@\"}; }' ) >/dev/null 2>&1; then 'eval' 'function git { '\\''local'\\'' _cc_skip= _cc_word; for _cc_word in ${1+\"$@\"}; do if [ -n \"$_cc_skip\" ]; then _cc_skip=; continue; fi; case \"$_cc_word\" in -C|-c|--attr-source|--shallow-file|--git-dir|--separate-git-dir|--work-tree|--namespace|--super-prefix|--config-env) _cc_skip=1 ;; -*) ;; grep) '\\''/usr/bin/env'\\'' LC_ALL=C '\\''git'\\'' ${1+\"$@\"}; '\\''return'\\'' ;; *) break ;; esac; done; '\\''command'\\'' '\\''git'\\'' ${1+\"$@\"}; }'; fi"
const claudeGitBlock = `{ 'eval' 'if ! '\''typeset'\'' -f '\''git'\'' >/dev/null 2>&1 && ! '\''alias'\'' '\''git'\'' >/dev/null 2>&1 && ( '\''eval'\'' '\''function git { '\''\'\'''\''local'\''\'\'''\'' _cc_skip= _cc_word; for _cc_word in ${1+"$@"}; do if [ -n "$_cc_skip" ]; then _cc_skip=; continue; fi; case "$_cc_word" in -C|-c|--attr-source|--shallow-file|--git-dir|--separate-git-dir|--work-tree|--namespace|--super-prefix|--config-env) _cc_skip=1 ;; -*) ;; grep) '\''\'\'''\''/usr/bin/env'\''\'\'''\'' LC_ALL=C '\''\'\'''\''git'\''\'\'''\'' ${1+"$@"}; '\''\'\'''\''return'\''\'\'''\'' ;; *) break ;; esac; done; '\''\'\'''\''command'\''\'\'''\'' '\''\'\'''\''git'\''\'\'''\'' ${1+"$@"}; }'\'' ) >/dev/null 2>&1; then '\''eval'\'' '\''function git { '\''\'\'''\''local'\''\'\'''\'' _cc_skip= _cc_word; for _cc_word in ${1+"$@"}; do if [ -n "$_cc_skip" ]; then _cc_skip=; continue; fi; case "$_cc_word" in -C|-c|--attr-source|--shallow-file|--git-dir|--separate-git-dir|--work-tree|--namespace|--super-prefix|--config-env) _cc_skip=1 ;; -*) ;; grep) '\''\'\'''\''/usr/bin/env'\''\'\'''\'' LC_ALL=C '\''\'\'''\''git'\''\'\'''\'' ${1+"$@"}; '\''\'\'''\''return'\''\'\'''\'' ;; *) break ;; esac; done; '\''\'\'''\''command'\''\'\'''\'' '\''\'\'''\''git'\''\'\'''\'' ${1+"$@"}; }'\''; fi' 2>/dev/null || 'true'; } && `

const claudeWhich = `{ \builtin unalias -- 'which'; \builtin unset -f -- 'which'; } >/dev/null 2>&1 || true && `

// ClaudePrefixLine recognizes only the observed Claude bootstrap. Everything
// else retains the ordinary whole-line policy, including unknown source/eval.
func ClaudePrefixLine(i Input, line string) string {
	tree, err := syntax.NewParser().Parse(strings.NewReader(line), "claude prefix")
	if err != nil {
		return line
	}
	var snapshot, payload, cwd string
	counts := map[string]int{}
	syntax.Walk(tree, func(n syntax.Node) bool {
		stmt, ok := n.(*syntax.Stmt)
		if !ok {
			return true
		}
		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		name, ok := claudeLiteral(call.Args[0])
		if !ok {
			return true
		}
		switch name {
		case "source", "eval":
			if len(call.Args) != 2 {
				return true
			}
			value, ok := claudeLiteral(call.Args[1])
			if !ok {
				return true
			}
			if name == "eval" && value == claudeGitBody {
				return true
			}
			counts[name]++
			if name == "source" {
				snapshot = value
				call.Args[1] = claudeWord("IKESU_SNAPSHOT")
			} else {
				payload = value
				call.Args[1] = claudeWord("IKESU_PAYLOAD")
			}
		case "pwd":
			counts[name]++
			if len(stmt.Redirs) != 1 {
				return true
			}
			value, ok := claudeLiteral(stmt.Redirs[0].Word)
			if !ok {
				return true
			}
			cwd = value
			stmt.Redirs[0].Word = claudeWord("IKESU_CWD")
		}
		return true
	})
	if counts["source"] > 1 || counts["eval"] != 1 || counts["pwd"] != 1 {
		return line
	}
	config := i.Env["CLAUDE_CONFIG_DIR"]
	if config == "" {
		config = filepath.Join(i.Env["HOME"], ".claude")
	}
	if counts["source"] == 1 && (!filepath.IsAbs(config) || filepath.Dir(snapshot) != filepath.Join(config, "shell-snapshots") || !strings.HasPrefix(filepath.Base(snapshot), "snapshot-zsh-") || !strings.HasSuffix(snapshot, ".sh")) {
		return line
	}
	if !has([]string{"/tmp", "/private/tmp"}, filepath.Dir(cwd)) || !strings.HasPrefix(filepath.Base(cwd), "claude-") || !strings.HasSuffix(cwd, "-cwd") {
		return line
	}
	template := claudeBootstrap
	if counts["source"] == 0 {
		template = strings.TrimPrefix(template, "source IKESU_SNAPSHOT 2>/dev/null || true && ")
	}
	var got bytes.Buffer
	// The payload has been replaced; ignore its original line positions when
	// comparing the remaining bootstrap syntax.
	printer := syntax.NewPrinter(syntax.SingleLine(true))
	if printer.Print(&got, tree) != nil {
		return line
	}
	withWhich := strings.Replace(template, `{ \builtin unalias -- 'unsetenv'`, claudeWhich+`{ \builtin unalias -- 'unsetenv'`, 1)
	for _, base := range []string{template, withWhich} {
		for _, git := range []bool{false, true} {
			candidate := base
			if git {
				candidate = strings.Replace(candidate, "eval IKESU_PAYLOAD", claudeGitBlock+"eval IKESU_PAYLOAD", 1)
			}
			for _, devnull := range []bool{true, false} {
				variant := candidate
				if !devnull {
					variant = strings.Replace(variant, " < /dev/null", "", 1)
				}
				expected, err := syntax.NewParser().Parse(strings.NewReader(variant), "expected")
				if err != nil {
					return line
				}
				var want bytes.Buffer
				if printer.Print(&want, expected) == nil && got.String() == want.String() {
					return payload
				}
			}
		}
	}
	return line
}

func claudeWord(value string) *syntax.Word {
	return &syntax.Word{Parts: []syntax.WordPart{&syntax.Lit{Value: value}}}
}
func claudeLiteral(word *syntax.Word) (string, bool) {
	if !claudeLiteralParts(word.Parts, false) {
		return "", false
	}
	values, err := expand.Fields(&expand.Config{Env: expand.ListEnviron(), NoUnset: true}, word)
	if err != nil || len(values) != 1 {
		return "", false
	}
	return values[0], true
}

func claudeLiteralParts(parts []syntax.WordPart, quoted bool) bool {
	for _, part := range parts {
		switch p := part.(type) {
		case *syntax.Lit:
			if !quoted && strings.ContainsAny(p.Value, "*?[{~") {
				return false
			}
		case *syntax.SglQuoted:
			if p.Dollar {
				return false
			}
		case *syntax.DblQuoted:
			if !claudeLiteralParts(p.Parts, true) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// PrepareClaude is used only for a directly launched Claude process. Shell
// launches use the matching function so ordinary panes and Codex keep their env.
func (a *Assets) PrepareClaude(env []string) []string {
	if !a.injected {
		return env
	}
	m := EnvMap(env)
	old, exists := m["CLAUDE_CODE_SHELL_PREFIX"]
	prefix := filepath.Join(a.Root, "guard", "shell", a.Pane, "claude-prefix")
	m["IKESU_GUARD_CLAUDE_PREFIX_ORIG"] = old
	m["IKESU_GUARD_CLAUDE_PREFIX_SET"] = ""
	if exists {
		m["IKESU_GUARD_CLAUDE_PREFIX_SET"] = "x"
	}
	m["CLAUDE_CODE_SHELL_PREFIX"] = prefix
	m["IKESU_GUARD_CLAUDE_PREFIX"] = prefix
	return environment(m)
}
