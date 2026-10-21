#!/bin/sh
set -eu
: "${IKESU_PARITY_BIN:=${GTX_PARITY_BIN:-}}"
: "${IKESU_PARITY_BIN:?set IKESU_PARITY_BIN or GTX_PARITY_BIN to the binary being compared}"
: "${PARITY_FIXTURE_DIR:?set the isolated fixture script directory}"
work="$HOME/bench-terrace.noindex/work"
python3 "$PARITY_FIXTURE_DIR/menu_cases.py" "$work/parity-menu-kinds-steps"
for steps in "$work"/parity-menu-kinds-steps/*.steps; do
 name=$(basename "$steps" .steps)
 python3 "$PARITY_FIXTURE_DIR/observe.py" --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$IKESU_PARITY_BIN" --ptyrun "$work/ptyrun-parity-cells" --steps "$steps" --out "$work/parity-menu-kinds/$name"
done
