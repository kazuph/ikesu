#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
(cd tools/bench/ptyrun && go build -o "$work/ptyrun-parity-details" .)
for scenario in chrome settings-detail copy-detail confirm-detail; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --gtx "$work/gtx-parity-next" --ptyrun "$work/ptyrun-parity-details" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-detail"
done
