#!/bin/sh
set -eu
: "${GTX_PARITY_BIN:?set the binary being compared}"
: "${PARITY_FIXTURE_DIR:?set the isolated fixture script directory}"
work="$HOME/bench-terrace.noindex/work"
for scenario in copy-clean-cells copy-key-details copy-selection-keys copy-page-sizes copy-wheel-cursor copy-unicode copy-cycle copy-colored-selection copy-wide-motions copy-resize-top copy-resize-middle copy-resize-bottom; do
 python3 "$PARITY_FIXTURE_DIR/observe.py" --clipboard-sandbox --herdr "$work/herdr-0.2.24" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-parity-cells" --steps "$PARITY_FIXTURE_DIR/$scenario.steps" --out "$work/parity-$scenario-fixed"
done
