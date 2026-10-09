#!/bin/sh
set -eu
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
for scenario in menus-dialog menu-select paste confirm-detail; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-next" --ptyrun "$work/ptyrun-parity" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-input-retest"
done
python3 tests/parity/observe.py --server --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-next" --ptyrun "$work/ptyrun-parity" --steps tests/parity/popup.steps --out "$work/parity-popup-input-retest"
