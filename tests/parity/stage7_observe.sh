#!/bin/sh
set -eu
out=$1
mode=${2---server}
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
work="$HOME/bench-terrace.noindex/work/detail"
go build -o "$work/gtx-stage7" ./cmd/gtx
python3 tests/parity/stage7_cases.py tests/parity/stage7_cases
for procedure in tests/parity/stage7_cases/*.steps; do
  name=$(basename "$procedure" .steps)
  python3 tests/parity/observe.py --herdr "$work/herdr-0.2.57" --gtx "$work/gtx-stage7" --ptyrun "$work/ptyrun" --config-file "$work/user-config.toml" --steps "$procedure" --out "$out/$name" --clipboard-sandbox $mode
 done

env -u SSH_CONNECTION -u SSH_CLIENT -u SSH_TTY python3 tests/parity/observe.py --herdr "$work/herdr-0.2.57" --gtx "$work/gtx-stage7" --ptyrun "$work/ptyrun" --config-file "$work/user-config.toml" --steps tests/parity/stage7_cases/k30-fallback.steps --out "$out/k30-local-sandbox" --clipboard-sandbox $mode
