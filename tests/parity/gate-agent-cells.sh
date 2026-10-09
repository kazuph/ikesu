#!/bin/sh
set -eu
: "${PARITY_CONTROLLER_SESSION_ID:?set the actual controller session ID}"
export PARITY_CONTROLLER_SESSION_ID
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-target/release/herdr"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
: "${GTX_PARITY_BIN:=$work/gtx-parity-agent-overflow}"
for scenario in agent-server-overflow agent-server-multi popup-owner-ui; do
 python3 tests/parity/observe.py --server --server-reuse-initial --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-cells"
 python3 tests/parity/full_frame_gate.py "$work/parity-$scenario-cells" "tests/parity/$scenario.steps"
done
python3 tests/parity/observe.py --server --server-reuse-initial --cell-clock --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps tests/parity/agent-working-clock.steps --out "$work/parity-agent-working-cells-final"
python3 tests/parity/agent_clock_gate.py "$work/parity-agent-working-cells-final"
