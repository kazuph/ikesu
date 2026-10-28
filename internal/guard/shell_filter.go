package guard

// These helpers inspect syntax and variable attributes using Bash builtins.
// They never evaluate the inspected text or change the caller's IFS/match state.
const bashSimpleForms = `_ikesu_guard_reserved_name() {
 case "$1" in _ikesu_guard_*|AI_GUARD_*|IKESU_*|BASH_ENV|ZDOTDIR|ENV|PATH|CLAUDE_CODE_SHELL_PREFIX) return 0;; esac
 return 1
}
_ikesu_guard_plain_variable() {
 local _ikesu_guard_name="$1" _ikesu_guard_decl _ikesu_guard_flags
 case "$_ikesu_guard_name" in ''|[!a-zA-Z_]*|*[!a-zA-Z0-9_]*) return 1;; esac
 _ikesu_guard_reserved_name "$_ikesu_guard_name" && return 1
 _ikesu_guard_decl=$(builtin declare -p "$_ikesu_guard_name" 2>/dev/null) || return 0
 _ikesu_guard_flags="${_ikesu_guard_decl#declare }"
 _ikesu_guard_flags="${_ikesu_guard_flags%% *}"
 [[ -z ${_ikesu_guard_flags//[-rx]/} ]]
}
_ikesu_guard_numeric_form() {
 local _ikesu_guard_expr="$1" _ikesu_guard_name _ikesu_guard_value IFS=' '
 case "$_ikesu_guard_expr" in *'['*|*'$'*|*'` + "`" + `'*|*'"'*|*"'"*|*';'*) return 1;; esac
 _ikesu_guard_expr="${_ikesu_guard_expr//[!a-zA-Z0-9_]/ }"
 for _ikesu_guard_name in $_ikesu_guard_expr; do
  case "$_ikesu_guard_name" in [0-9]*) continue;; esac
  _ikesu_guard_plain_variable "$_ikesu_guard_name" || return 1
  _ikesu_guard_value="${!_ikesu_guard_name}"
  case "$_ikesu_guard_value" in *[!0-9+-]*) return 1;; esac
 done
 return 0
}
_ikesu_guard_simple_form() {
 local _ikesu_guard_text="$1" _ikesu_guard_char _ikesu_guard_quote='' _ikesu_guard_escape=0
 local _ikesu_guard_token='' _ikesu_guard_name _ikesu_guard_declaration=0 _ikesu_guard_seen=0
 case "$_ikesu_guard_text" in
  '(('*'))') _ikesu_guard_numeric_form "${_ikesu_guard_text:2:${#_ikesu_guard_text}-4}"; return $?;;
 esac
 if [[ $_ikesu_guard_text == '[[ '* && $_ikesu_guard_text == *' ]]' ]]; then
  _ikesu_guard_text="${_ikesu_guard_text:3:${#_ikesu_guard_text}-6}"
  case "$_ikesu_guard_text" in *AI_GUARD_*|*IKESU_*|*_ikesu_guard_*|*BASH_ENV*|*ZDOTDIR*|*'${ENV'*|*'${PATH'*|*CLAUDE_CODE_SHELL_PREFIX*|*'['*|*'('*) return 1;; esac
  case " $_ikesu_guard_text " in *' -eq '*|*' -ne '*|*' -gt '*|*' -ge '*|*' -lt '*|*' -le '*)
   _ikesu_guard_text="${_ikesu_guard_text//\$/}"
   _ikesu_guard_text="${_ikesu_guard_text//\"/}"
   _ikesu_guard_text="${_ikesu_guard_text//\'/}"
   _ikesu_guard_numeric_form "$_ikesu_guard_text"; return $?;; esac
  return 0
 fi
 case "$_ikesu_guard_text" in local\ *|declare\ *)
  _ikesu_guard_declaration=1
  _ikesu_guard_text="${_ikesu_guard_text#* }";;
 esac
 # Split only at unquoted whitespace. Any unquoted shell operator or unfinished
 # quoting is uncertain and goes to the full parser instead.
 _ikesu_guard_text+=' '
 while [[ -n $_ikesu_guard_text ]]; do
  _ikesu_guard_char="${_ikesu_guard_text:0:1}"
  _ikesu_guard_text="${_ikesu_guard_text:1}"
  if [[ $_ikesu_guard_escape == 1 ]]; then
   _ikesu_guard_token+="$_ikesu_guard_char"; _ikesu_guard_escape=0; continue
  fi
  if [[ -n $_ikesu_guard_quote ]]; then
   _ikesu_guard_token+="$_ikesu_guard_char"
   if [[ $_ikesu_guard_char == "$_ikesu_guard_quote" ]]; then _ikesu_guard_quote=''
   elif [[ $_ikesu_guard_char == '\' && $_ikesu_guard_quote == '"' ]]; then _ikesu_guard_escape=1; fi
   continue
  fi
  case "$_ikesu_guard_char" in
   '\') _ikesu_guard_escape=1; _ikesu_guard_token+="$_ikesu_guard_char";;
   "'"|'"') _ikesu_guard_quote="$_ikesu_guard_char"; _ikesu_guard_token+="$_ikesu_guard_char";;
   ' '|$'\t'|$'\r'|$'\n')
    [[ -z $_ikesu_guard_token ]] && continue
    if [[ $_ikesu_guard_declaration == 1 && $_ikesu_guard_seen == 0 && $_ikesu_guard_token == -- ]]; then _ikesu_guard_token=''; continue; fi
    _ikesu_guard_name="${_ikesu_guard_token%%=*}"
    _ikesu_guard_name="${_ikesu_guard_name%+}"
    if [[ $_ikesu_guard_token != *=* && $_ikesu_guard_declaration != 1 ]]; then return 1; fi
    _ikesu_guard_plain_variable "$_ikesu_guard_name" || return 1
    _ikesu_guard_seen=1; _ikesu_guard_token='';;
   ';'|'&'|'|'|'<'|'>'|'('|')'|'` + "`" + `') return 1;;
   *) _ikesu_guard_token+="$_ikesu_guard_char";;
  esac
 done
 [[ $_ikesu_guard_seen == 1 && -z $_ikesu_guard_quote && $_ikesu_guard_escape == 0 ]]
}
readonly -f _ikesu_guard_reserved_name _ikesu_guard_plain_variable _ikesu_guard_numeric_form _ikesu_guard_simple_form
`
