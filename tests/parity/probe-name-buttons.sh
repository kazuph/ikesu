#!/bin/sh
set -eu
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
for action in save clear cancel; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "${IKESU_PARITY_BIN:-$work/ikesu-parity-name-provisional}" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/name-button-$action.steps" --out "$work/parity-name-button-$action"
done
