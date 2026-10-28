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
 case "${FUNCNAME[2]:-}" in _ikesu_guard_forward|_ikesu_guard_command) return 0;; esac
 if [[ %s ]]; then
  (
   set +o functrace
   builtin compgen -v | while IFS= builtin read -r _ikesu_guard_snapshot_name; do
    case "$_ikesu_guard_snapshot_name" in _ikesu_guard_*) continue;; esac
    builtin declare -p "$_ikesu_guard_snapshot_name"
   done
  ) | %s guard line --via bash-debug -- "$1" || { builtin kill -s KILL "$$"; exit 1; }
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
readonly -f _ikesu_guard_debug _ikesu_guard_dispatch
set -o functrace
trap '_ikesu_guard_dispatch "$BASH_COMMAND"' DEBUG
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
