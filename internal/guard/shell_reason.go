package guard

// BASH_ENV can run a user loader which deliberately unsets the request reason.
// Pin only this request context before that loader, separate from snapshots.
const bashRequestReasonCapture = `if [[ $- != *i* ]]; then
_ikesu_guard_reason_name="${AI_GUARD_REQUEST_ENV_NAME:-AI_GUARD_REQUEST_REASON}"
case "$_ikesu_guard_reason_name" in ''|[!a-zA-Z_]*|*[!a-zA-Z0-9_]*) exit 1;; esac
_ikesu_guard_request_reason="${!_ikesu_guard_reason_name-}"
_ikesu_guard_parent_reason="${IKESU_GUARD_PARENT_REASON:-}"
readonly _ikesu_guard_reason_name _ikesu_guard_request_reason _ikesu_guard_parent_reason
fi
`

const bashRequestReasonRestore = `builtin export "AI_GUARD_REQUEST_ENV_NAME=$_ikesu_guard_reason_name" "$_ikesu_guard_reason_name=$_ikesu_guard_request_reason" "IKESU_GUARD_PARENT_REASON=$_ikesu_guard_parent_reason" || exit 1
`
