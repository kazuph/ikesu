package guard

import (
	"fmt"
	"strings"
)

func (a *Assets) debugHook(shell string) string {
	if shell == "bash" {
		targets := make([]string, 0, len(a.Config.Commands()))
		for _, name := range a.Config.Commands() {
			targets = append(targets, quote(name))
		}
		protectForward := ""
		if len(targets) > 0 {
			protectForward = "readonly -f _ikesu_guard_forward _ikesu_guard_command\n"
		}
		return fmt.Sprintf(`if [[ $- != *i* ]]; then
if [[ %s ]] && [[ -n "${BASH_EXECUTION_STRING:-}" ]]; then
 %s guard line -- "$BASH_EXECUTION_STRING" || exit $?
fi
%s%s_ikesu_guard_needs_check() {
 local _ikesu_guard_line="$1" _ikesu_guard_word
 _ikesu_guard_line="${_ikesu_guard_line#"${_ikesu_guard_line%%%%[!$' \t\r\n']*}"}"
 _ikesu_guard_line="${_ikesu_guard_line%%%%"${_ikesu_guard_line##*[!$' \t\r\n']}"}"
 _ikesu_guard_simple_form "$_ikesu_guard_line" && return 1
 case "$_ikesu_guard_line" in *'>'*) return 0;; esac
 _ikesu_guard_word="${_ikesu_guard_line%%%%[$' \t\r\n']*}"
 case "$_ikesu_guard_word" in ''|*[!a-zA-Z0-9_./-]*) return 0;; esac
 _ikesu_guard_word="${_ikesu_guard_word##*/}"
 case "$_ikesu_guard_word" in
 %s) return 0;;
 xargs|find|sh|bash|zsh|env|command|builtin|exec|eval|source|.|sudo|doas|timeout|stdbuf|arch|chroot|setsid|ionice) return 0;;
 trap|unset|unsetenv|setenv|export|typeset|declare|local|readonly|unfunction|functions|alias|unalias|set|setopt|unsetopt|shopt|chmod|chown|chgrp|sed|truncate|mkdir|install) return 0;;
 esac
 return 1
}
_ikesu_guard_debug() {
 local _ikesu_guard_debug_busy=1
 case "${FUNCNAME[2]:-}" in _ikesu_guard_forward|_ikesu_guard_command) return 0;; esac
 if [[ %s ]] && _ikesu_guard_needs_check "$1"; then
  (
   set +o functrace
   builtin compgen -v | while IFS= builtin read -r _ikesu_guard_snapshot_name; do
    case "$_ikesu_guard_snapshot_name" in _ikesu_guard_*) continue;; esac
    builtin declare -p "$_ikesu_guard_snapshot_name"
   done
  ) | (
   set +o functrace
%s   %s guard line --via bash-debug -- "$1"
  ) || { builtin kill -s KILL "$$"; exit 1; }
 fi
 return 0
}
_ikesu_guard_dispatch() {
 local _ikesu_guard_frame
 if [[ ${_ikesu_guard_debug_busy:-} == 1 ]]; then
  for _ikesu_guard_frame in "${FUNCNAME[@]}"; do
   [[ $_ikesu_guard_frame == _ikesu_guard_debug ]] && return 0
  done
 fi
 _ikesu_guard_debug "$1"
}
readonly -f _ikesu_guard_debug _ikesu_guard_dispatch _ikesu_guard_needs_check
set -o functrace
trap '_ikesu_guard_dispatch "$BASH_COMMAND"' DEBUG
fi
`, a.shellActive(), a.invocation(), protectForward, bashSimpleForms, strings.Join(append(targets, quote("")), "|"), a.shellActive(), bashRequestReasonRestore, a.invocation())
	}
	return fmt.Sprintf(`if [[ ! -o interactive ]]; then
if [[ %s ]] && [[ -n "${ZSH_EXECUTION_STRING:-}" ]]; then
 %s guard line -- "$ZSH_EXECUTION_STRING" || exit $?
fi
setopt DEBUG_BEFORE_CMD
TRAPDEBUG() {
 case "${funcstack[2]:-}" in _ikesu_guard_forward|_ikesu_guard_command) return 0;; esac
 if [[ %s ]]; then
  %s guard line -- "$ZSH_DEBUG_CMD" || exit $?
 fi
 return 0
}
fi
`, a.shellActive(), a.invocation(), a.shellActive(), a.invocation())
}
