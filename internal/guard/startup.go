package guard

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigureShell is called before Prepare for the interactive pane shell.
// A direct agent receives the already completed PATH without a prompt hook.
func (a *Assets) ConfigureShell(shell string) {
	switch filepath.Base(shell) {
	case "zsh":
	case "bash":
		a.prompt = true
	default:
		a.unsupported = "shell の起動後に shim の PATH を保証できません: " + shell
	}
}

const pinPath = `case "$PATH" in "$IKESU_GUARD_BIN"|"$IKESU_GUARD_BIN":*) ;; *) export PATH="$IKESU_GUARD_BIN:$PATH";; esac
`

func (a *Assets) prepareStartup(m map[string]string, dir string) error {
	prefix := "#!/bin/sh\n" + a.identity() + " exec " + quote(a.Executable) + " guard claude-prefix -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "claude-prefix"), []byte(prefix), 0700); err != nil {
		return err
	}
	wrappers := a.shellFunctions() + a.agentFunctions(dir)
	m["IKESU_GUARD_ZDOTDIR_ORIG"], m["IKESU_GUARD_ZDOTDIR_SET"] = m["ZDOTDIR"], "0"
	if _, ok := m["ZDOTDIR"]; ok {
		m["IKESU_GUARD_ZDOTDIR_SET"] = "1"
	}
	m["IKESU_GUARD_SHELL_DIR"] = dir
	restore := `if [[ "$IKESU_GUARD_ZDOTDIR_SET" == 1 ]]; then export ZDOTDIR="$IKESU_GUARD_ZDOTDIR_ORIG"; else unset ZDOTDIR; fi
`
	capture := `IKESU_GUARD_ZDOTDIR_ORIG="${ZDOTDIR:-}"; IKESU_GUARD_ZDOTDIR_SET=${+ZDOTDIR}
export IKESU_GUARD_ZDOTDIR_ORIG IKESU_GUARD_ZDOTDIR_SET
`
	// Each original file sees the user's ZDOTDIR. Only the intervals between
	// startup files use our directory; the final phase restores it permanently.
	phases := []struct{ name, next string }{
		{".zshenv", "[[ -o login || -o interactive ]]"},
		{".zprofile", "true"},
		{".zshrc", "[[ -o login ]]"},
		{".zlogin", "false"},
	}
	for _, phase := range phases {
		body := restore + fmt.Sprintf("if [[ -f \"${ZDOTDIR:-$HOME}/%s\" ]]; then source \"${ZDOTDIR:-$HOME}/%s\"; fi\n", phase.name, phase.name) + capture + pinPath
		if phase.name == ".zshenv" {
			body += wrappers + a.lineHook()
		}
		body += "if " + phase.next + "; then export ZDOTDIR=\"$IKESU_GUARD_SHELL_DIR\"; fi\n"
		if phase.name == ".zshenv" {
			body += "if [[ ! -o login && ! -o interactive ]]; then\n" + a.debugHook("zsh") + "fi\n"
		}
		if phase.name == ".zlogin" {
			body += a.debugHook("zsh")
		}

		if e := os.WriteFile(filepath.Join(dir, phase.name), []byte(body), 0600); e != nil {
			return e
		}
	}
	m["ZDOTDIR"] = dir
	m["IKESU_GUARD_BASH_ENV_ORIG"] = m["BASH_ENV"]
	bash := a.bashStartup(dir)
	if e := os.WriteFile(filepath.Join(dir, "bash-env"), []byte(bash), 0600); e != nil {
		return e
	}
	m["BASH_ENV"] = filepath.Join(dir, "bash-env")
	if a.prompt {
		// Interactive bash ignores BASH_ENV. Its first prompt runs after both the
		// system and user login files. Without this acknowledgement no marker is
		// published and ai_guard is not suppressed in a child shell.
		m["IKESU_GUARD_PROMPT_COMMAND_ORIG"], m["IKESU_GUARD_PROMPT_COMMAND_SET"] = m["PROMPT_COMMAND"], "0"
		if _, ok := m["PROMPT_COMMAND"]; ok {
			m["IKESU_GUARD_PROMPT_COMMAND_SET"] = "1"
		}
		m["IKESU_GUARD_READY"] = filepath.Join(dir, "ready")
		_ = os.Remove(m["IKESU_GUARD_READY"])
		hook := pinPath
		if a.Config.EffectiveMode() == "enforce" {
			hook += "export IKESU_GUARD_ENFORCING=1\n"
		}
		hook += `if [[ ! -f "$IKESU_GUARD_READY" ]]; then builtin printf '' > "$IKESU_GUARD_READY"; fi
`
		m["PROMPT_COMMAND"] = m["PROMPT_COMMAND"] + "\n" + hook
		delete(m, "IKESU_GUARD_ENFORCING")
	}
	if a.unsupported != "" {
		delete(m, "IKESU_GUARD_ENFORCING")
	}
	return nil
}

// Keep the user's ZDOTDIR in their interactive shell, while external agents
// inherit the generated startup chain for every tool shell they launch.
func (a *Assets) agentFunctions(dir string) string {
	var body string
	for _, agent := range []string{"codex", "claude"} {
		invocation := `ZDOTDIR=` + quote(dir) + ` BASH_ENV=` + quote(filepath.Join(dir, "bash-env")) + ` builtin command ` + agent + ` "$@"`
		if agent == "claude" {
			invocation = `local _ikesu_claude_prefix=` + quote(filepath.Join(dir, "claude-prefix")) + `
 IKESU_GUARD_CLAUDE_PREFIX_ORIG="${CLAUDE_CODE_SHELL_PREFIX:-}" IKESU_GUARD_CLAUDE_PREFIX_SET="${CLAUDE_CODE_SHELL_PREFIX+x}" IKESU_GUARD_CLAUDE_PREFIX="$_ikesu_claude_prefix" CLAUDE_CODE_SHELL_PREFIX="$_ikesu_claude_prefix" ` + invocation
		}
		body += fmt.Sprintf(`if ! typeset -f %[1]s >/dev/null 2>&1 && ! alias %[1]s >/dev/null 2>&1; then
%[1]s() { %[2]s; }
fi
`, agent, invocation)
	}
	return body
}

// Use the same generator when proving a child bash will read our DEBUG hook.
func (a *Assets) bashStartup(dir string) string {
	restore := `if [[ "$IKESU_GUARD_ZDOTDIR_SET" == 1 ]]; then export ZDOTDIR="$IKESU_GUARD_ZDOTDIR_ORIG"; else unset ZDOTDIR; fi
`
	return fmt.Sprintf("if [[ %s ]]; then\n", a.shellActive()) + bashRequestReasonCapture + "fi\n" + restore + `if [[ -n "$IKESU_GUARD_BASH_ENV_ORIG" && -f "$IKESU_GUARD_BASH_ENV_ORIG" ]]; then source "$IKESU_GUARD_BASH_ENV_ORIG"; fi
` + "if [[ $- != *i* && ${_ikesu_guard_reason_name+x} ]]; then\n" + bashRequestReasonRestore + "fi\n" + pinPath + a.shellFunctions() + a.agentFunctions(dir) + a.debugHook("bash")
}
