#!/bin/sh
set -eu
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
for scenario in prefix-routing sidebar-detail navigate-detail resize-detail; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --gtx "$work/gtx-parity-input-final" --ptyrun "$work/ptyrun-parity" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-before"
done
python3 tests/parity/observe.py --server --clipboard-sandbox --herdr "$work/herdr-0.2.24" --gtx "$work/gtx-parity-input-final" --ptyrun "$work/ptyrun-parity" --steps tests/parity/popup-detail.steps --out "$work/parity-popup-detail-before"
