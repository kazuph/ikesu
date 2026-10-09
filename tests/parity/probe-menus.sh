#!/bin/sh
set -eu
: "${GTX_PARITY_BIN:?set the binary being compared}"
: "${PARITY_FIXTURE_DIR:?set the isolated fixture script directory}"
work="$HOME/bench-terrace.noindex/work"
python3 "$PARITY_FIXTURE_DIR/menu_cases.py" "$work/parity-menu-kinds-steps"
for steps in "$work"/parity-menu-kinds-steps/*.steps; do
 name=$(basename "$steps" .steps)
 python3 "$PARITY_FIXTURE_DIR/observe.py" --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-parity-cells" --steps "$steps" --out "$work/parity-menu-kinds/$name"
done
