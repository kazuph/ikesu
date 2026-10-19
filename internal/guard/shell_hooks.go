package guard

import "fmt"

func (a *Assets) debugHook(shell string) string {
	if shell == "bash" {
		return fmt.Sprintf(`if [[ $- != *i* ]]; then
_gtx_guard_debug() {
 case "${FUNCNAME[1]:-}" in _gtx_guard_forward|_gtx_guard_command) return 0;; esac
 if [[ %s ]]; then
  %s guard line -- "$1" || exit $?
 fi
 return 0
}
shopt -s extdebug
set -o functrace
trap '_gtx_guard_debug "$BASH_COMMAND"' DEBUG
fi
`, a.shellActive(), a.invocation())
	}
	return fmt.Sprintf(`if [[ ! -o interactive ]]; then
setopt DEBUG_BEFORE_CMD
TRAPDEBUG() {
 case "${funcstack[2]:-}" in _gtx_guard_forward|_gtx_guard_command) return 0;; esac
 if [[ %s ]]; then
  %s guard line -- "$ZSH_DEBUG_CMD" || exit $?
 fi
 return 0
}
fi
`, a.shellActive(), a.invocation())
}
