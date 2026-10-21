#!/bin/sh
set -eu
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
for scenario in settings-height-sections settings-size; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "${IKESU_PARITY_BIN:-${GTX_PARITY_BIN:-$work/gtx-parity-settings-adaptive}}" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-adaptive"
done
