#!/bin/sh
set -eu
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
python3 tests/parity/navigator_cases.py "$work/parity-navigator-steps"
for steps in "$work"/parity-navigator-steps/*.steps; do
 name=$(basename "$steps" .steps)
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "${GTX_PARITY_BIN:-$work/gtx-parity-navigator}" --ptyrun "$work/ptyrun-parity-cells" --steps "$steps" --out "$work/parity-navigator/$name"
 python3 tests/parity/navigator_compare.py "$work/parity-navigator/$name"
 case "$name" in pane-enter|query-enter) python3 tests/parity/focus_compare.py "$work/parity-navigator/$name";; esac
done
