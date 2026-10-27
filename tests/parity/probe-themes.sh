#!/bin/sh
set -eu
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
python3 tests/parity/theme_cases.py "$work/parity-theme-steps"
for steps in "$work"/parity-theme-steps/*.steps; do
 name=$(basename "$steps" .steps)
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-detail" --ptyrun "$work/ptyrun-parity-cells" --steps "$steps" --out "$work/parity-themes/$name"
done
