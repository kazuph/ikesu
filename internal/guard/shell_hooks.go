package guard

import "fmt"

func (a *Assets) debugHook(shell string) string {
	if shell == "bash" {
		return fmt.Sprintf(`if [[ $- != *i* ]]; then
if [[ %s ]] && [[ -n "${BASH_EXECUTION_STRING:-}" ]]; then
 %s guard line -- "$BASH_EXECUTION_STRING" || exit $?
fi
_ikesu_guard_debug() {
 local _ikesu_guard_debug_busy=1
 case "${FUNCNAME[1]:-}" in _ikesu_guard_forward|_ikesu_guard_command) return 0;; esac
 if [[ %s ]]; then
  (
   set +o functrace
   for _ikesu_guard_snapshot_name in $(builtin compgen -v); do
   case "$_ikesu_guard_snapshot_name" in _ikesu_guard_*) continue;; esac
   builtin declare -p "$_ikesu_guard_snapshot_name"
   done
  ) | %s guard line --via bash-debug -- "$1" || { builtin kill -s TERM "$$"; exit 1; }
 fi
 return 0
}
readonly -f _ikesu_guard_debug
set -o functrace
trap '[[ ${_ikesu_guard_debug_busy:-} == 1 && " ${FUNCNAME[*]} " == *" _ikesu_guard_debug "* ]] || _ikesu_guard_debug "$BASH_COMMAND"' DEBUG
fi
`, a.shellActive(), a.invocation(), a.shellActive(), a.invocation())
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
