#!/bin/sh
set -eu
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
python3 tests/parity/mobile_cases.py "$work/parity-mobile-steps"
for steps in "$work"/parity-mobile-steps/*.steps; do
 name=$(basename "$steps" .steps)
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --gtx "${GTX_PARITY_BIN:-$work/gtx-parity-sidebar}" --ptyrun "$work/ptyrun-parity-cells" --steps "$steps" --out "$work/parity-mobile-actions/$name"
done
