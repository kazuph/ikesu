#!/bin/sh
set -eu
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
fixtures="${PARITY_FIXTURE_DIR:-tests/parity}"
out="${PARITY_OUT_DIR:-$work/parity-prefix}"
python3 "$fixtures/prefix_cases.py" "$work/parity-prefix-steps"
for steps in "$work"/parity-prefix-steps/*.steps; do
 name=$(basename "$steps" .steps)
 python3 "$fixtures/observe.py" --herdr "$work/herdr-target/release/herdr" --gtx "${GTX_PARITY_BIN:-$work/gtx-parity-next}" --ptyrun "${PARITY_PTYRUN:-$work/ptyrun-parity}" --steps "$steps" --out "$out/$name"
done
