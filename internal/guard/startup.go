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

const pinPath = `case "$PATH" in "$GTX_GUARD_BIN"|"$GTX_GUARD_BIN":*) ;; *) export PATH="$GTX_GUARD_BIN:$PATH";; esac
`

func (a *Assets) prepareStartup(m map[string]string, dir string) error {
	wrappers := a.shellFunctions() + a.agentFunctions(dir)
	m["GTX_GUARD_ZDOTDIR_ORIG"], m["GTX_GUARD_ZDOTDIR_SET"] = m["ZDOTDIR"], "0"
	if _, ok := m["ZDOTDIR"]; ok {
		m["GTX_GUARD_ZDOTDIR_SET"] = "1"
	}
	m["GTX_GUARD_SHELL_DIR"] = dir
	restore := `if [[ "$GTX_GUARD_ZDOTDIR_SET" == 1 ]]; then export ZDOTDIR="$GTX_GUARD_ZDOTDIR_ORIG"; else unset ZDOTDIR; fi
`
	capture := `GTX_GUARD_ZDOTDIR_ORIG="${ZDOTDIR:-}"; GTX_GUARD_ZDOTDIR_SET=${+ZDOTDIR}
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
		body += "if " + phase.next + "; then export ZDOTDIR=\"$GTX_GUARD_SHELL_DIR\"; fi\n"
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
	m["GTX_GUARD_BASH_ENV_ORIG"] = m["BASH_ENV"]
	bash := restore + `if [[ -n "$GTX_GUARD_BASH_ENV_ORIG" && -f "$GTX_GUARD_BASH_ENV_ORIG" ]]; then source "$GTX_GUARD_BASH_ENV_ORIG"; fi
` + pinPath + wrappers + a.debugHook("bash")
	if e := os.WriteFile(filepath.Join(dir, "bash-env"), []byte(bash), 0600); e != nil {
		return e
	}
	m["BASH_ENV"] = filepath.Join(dir, "bash-env")
	if a.prompt {
		// Interactive bash ignores BASH_ENV. Its first prompt runs after both the
		// system and user login files. Without this acknowledgement no marker is
		// published and ai_guard is not suppressed in a child shell.
		m["GTX_GUARD_PROMPT_COMMAND_ORIG"], m["GTX_GUARD_PROMPT_COMMAND_SET"] = m["PROMPT_COMMAND"], "0"
		if _, ok := m["PROMPT_COMMAND"]; ok {
			m["GTX_GUARD_PROMPT_COMMAND_SET"] = "1"
		}
		m["GTX_GUARD_READY"] = filepath.Join(dir, "ready")
		_ = os.Remove(m["GTX_GUARD_READY"])
		hook := pinPath
		if a.Config.EffectiveMode() == "enforce" {
			hook += "export GTX_GUARD_ENFORCING=1\n"
		}
		hook += `if [[ ! -f "$GTX_GUARD_READY" ]]; then builtin printf '' > "$GTX_GUARD_READY"; fi
`
		m["PROMPT_COMMAND"] = m["PROMPT_COMMAND"] + "\n" + hook
		delete(m, "GTX_GUARD_ENFORCING")
	}
	if a.unsupported != "" {
		delete(m, "GTX_GUARD_ENFORCING")
	}
	return nil
}

// Keep the user's ZDOTDIR in their interactive shell, while external agents
// inherit the generated startup chain for every tool shell they launch.
func (a *Assets) agentFunctions(dir string) string {
	var body string
	for _, agent := range []string{"codex", "claude"} {
		body += fmt.Sprintf(`if ! typeset -f %[1]s >/dev/null 2>&1 && ! alias %[1]s >/dev/null 2>&1; then
%[1]s() { ZDOTDIR=%[2]s BASH_ENV=%[3]s builtin command %[1]s "$@"; }
fi
`, agent, quote(dir), quote(filepath.Join(dir, "bash-env")))
	}
	return body
}
