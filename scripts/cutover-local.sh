#!/bin/sh
# Run outside the terminal server: nohup + a new session are the caller's responsibility.
set -eu
exec python3 "$(dirname "$0")/cutover_local.py" "$@"
