#!/bin/sh
set -eu
: "${IKESU_PARITY_BIN:?set the binary being compared}"
: "${PARITY_FIXTURE_DIR:?set the isolated fixture script directory}"
work="$HOME/bench-terrace.noindex/work"
out="${PARITY_SIDEBAR_THEME_OUT:-$work/parity-sidebar-theme-cards}"
python3 "$PARITY_FIXTURE_DIR/sidebar_theme_cases.py" "$work/parity-sidebar-theme-steps"
for steps in "$work"/parity-sidebar-theme-steps/*.steps; do
 name=$(basename "$steps" .steps)
 python3 "$PARITY_FIXTURE_DIR/observe.py" --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$IKESU_PARITY_BIN" --ptyrun "$work/ptyrun-parity-cells" --steps "$steps" --out "$out/$name"
done
