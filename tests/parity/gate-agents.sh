#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-target/release/herdr"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
: "${PARITY_CONTROLLER_SESSION_ID:?set the actual controller session ID}"
export PARITY_CONTROLLER_SESSION_ID
test -z "$(gofmt -l internal cmd tests tools)"
export GTX_PARITY_BIN="$work/gtx-parity-agent-final"
go build -o "$GTX_PARITY_BIN" ./cmd/gtx
go vet ./...
go test -count=1 ./...
sh tests/parity/gate-agent-cells.sh
python3 tests/parity/observe.py --server --server-reuse-initial --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps tests/parity/agent-server-states.steps --out "$work/parity-agent-states-final"
python3 tests/parity/agent_states_gate.py "$work/parity-agent-states-final" "$work/parity-agent-working-cells-final"
for scenario in jobs-fine jobs-log-idle jobs-cancelling; do
 python3 tests/parity/observe.py --server --server-reuse-initial --cell-clock --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps "tests/parity/$scenario.steps" --out "$work/parity-agent-$scenario-final"
done
python3 tests/parity/jobs_age_gate.py "$work/parity-agent-jobs-fine-final"
python3 tests/parity/jobs_frame_gate.py "$work/parity-agent-jobs-fine-final"
python3 tests/parity/jobs_keys_gate.py "$work/parity-agent-jobs-log-idle-final"
python3 tests/parity/jobs_log_frame_gate.py "$work/parity-agent-jobs-log-idle-final"
python3 tests/parity/jobs_cancel_gate.py "$work/parity-agent-jobs-cancelling-final"
GTX_PERF_BIN="$GTX_PARITY_BIN" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-agent-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
