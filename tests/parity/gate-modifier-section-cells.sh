#!/bin/sh
set -eu
work="$HOME/bench-terrace.noindex/work"
cd "$HOME/bench-terrace.noindex/src/parity"
: "${GTX_PARITY_BIN:=$work/gtx-parity-modifier-section}"
for mode in local server; do
 if test "$mode" = server; then
  set -- --server --server-reuse-initial
 else
  set --
 fi
 for scenario in prefix-fast-escape navigator-fast-routing; do
  python3 tests/parity/observe.py "$@" --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-$mode-final"
  python3 tests/parity/full_frame_gate.py "$work/parity-$scenario-$mode-final" "tests/parity/$scenario.steps"
 done
done
for scenario in server-section-overflow server-section-scroll-end agent-server-overflow; do
 python3 tests/parity/observe.py --server --server-reuse-initial --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-modifier-final"
 python3 tests/parity/full_frame_gate.py "$work/parity-$scenario-modifier-final" "tests/parity/$scenario.steps"
done
