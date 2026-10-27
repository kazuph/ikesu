#!/bin/sh
set -eu
: "${PARITY_CONTROLLER_SESSION_ID:?set the actual controller session ID}"
export PARITY_CONTROLLER_SESSION_ID
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
: "${IKESU_PARITY_BIN:=$work/ikesu-parity-agent-overflow}"
for scenario in agent-server-overflow agent-server-multi popup-owner-ui; do
 python3 tests/parity/observe.py --server --server-reuse-initial --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$IKESU_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-cells"
 python3 tests/parity/full_frame_gate.py "$work/parity-$scenario-cells" "tests/parity/$scenario.steps"
done
python3 tests/parity/observe.py --server --server-reuse-initial --cell-clock --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$IKESU_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps tests/parity/agent-working-clock.steps --out "$work/parity-agent-working-cells-final"
python3 tests/parity/agent_clock_gate.py "$work/parity-agent-working-cells-final"
