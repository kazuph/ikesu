#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex/work/detail"
out="$1"
go build -o "$root/gtx-stage5" ./cmd/gtx
python3 tests/parity/stage5_cases.py "$out/procedures" "$root/user-config.toml"
for steps in "$out"/procedures/*.steps; do
 name=$(basename "$steps" .steps)
 python3 tests/parity/observe.py --herdr "$root/herdr-0.2.57" --gtx "$root/gtx-stage5" --ptyrun "$root/ptyrun" --config-file "$out/procedures/$name.toml" --steps "$steps" --out "$out/$name" --clipboard-sandbox --server
done
