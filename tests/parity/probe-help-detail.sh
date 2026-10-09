#!/bin/sh
set -eu
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
for scenario in help-complete help-mouse help-close; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "${GTX_PARITY_BIN:-$work/gtx-parity-settings-adaptive}" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-provisional"
 python3 tests/parity/navigator_compare.py "$work/parity-$scenario-provisional" --title keybinds --result help-comparison.json
done
