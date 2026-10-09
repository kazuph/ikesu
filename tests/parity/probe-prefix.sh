#!/bin/sh
set -eu
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
python3 tests/parity/prefix_cases.py "$work/parity-prefix-steps"
for steps in "$work"/parity-prefix-steps/*.steps; do
 name=$(basename "$steps" .steps)
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-next" --ptyrun "$work/ptyrun-parity" --steps "$steps" --out "$work/parity-prefix/$name"
done
