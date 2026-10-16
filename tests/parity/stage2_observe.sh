#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
out="$1"
go build -o "$root/work/detail/gtx-stage2" ./cmd/gtx
for procedure in tests/parity/stage2_cases/*.steps; do
  name=$(basename "$procedure" .steps)
  python3 tests/parity/observe.py --herdr "$root/work/detail/herdr-0.2.57" --gtx "$root/work/detail/gtx-stage2" --ptyrun "$root/work/detail/ptyrun" --config-file "$root/work/detail/user-config.toml" --server --clipboard-sandbox --steps "$procedure" --out "$out/$name"
done
