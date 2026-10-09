#!/bin/sh
set -eu
: "${PARITY_CONTROLLER_SESSION_ID:?set the actual controller session ID}"
export PARITY_CONTROLLER_SESSION_ID
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
test -z "$(gofmt -l internal cmd tests tools)"
go build -o "$work/gtx-parity-jobs" ./cmd/gtx
go build -o "$work/ptyrun-parity-jobs" tools/bench/ptyrun/main.go
go vet ./...
go test -count=1 ./...
for scenario in jobs-panel jobs-success jobs-log-keys jobs-log-idle jobs-fine jobs-cancel jobs-cancelling; do
 python3 tests/parity/observe.py --server --server-reuse-initial --cell-clock --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-jobs" --ptyrun "$work/ptyrun-parity-jobs" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-final"
done
python3 tests/parity/jobs_age_gate.py "$work/parity-jobs-panel-final"
python3 tests/parity/jobs_age_gate.py "$work/parity-jobs-success-final"
python3 tests/parity/jobs_age_gate.py "$work/parity-jobs-fine-final"
python3 tests/parity/jobs_frame_gate.py "$work/parity-jobs-fine-final"
python3 tests/parity/jobs_keys_gate.py "$work/parity-jobs-log-keys-final"
python3 tests/parity/jobs_keys_gate.py "$work/parity-jobs-log-idle-final"
python3 tests/parity/jobs_log_frame_gate.py "$work/parity-jobs-log-idle-final"
python3 tests/parity/jobs_cancel_gate.py "$work/parity-jobs-cancelling-final"
for scenario in popup-fine popup-width; do
 python3 tests/parity/observe.py --server --server-reuse-initial --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-jobs" --ptyrun "$work/ptyrun-parity-jobs" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-final"
 python3 tests/parity/full_frame_gate.py "$work/parity-$scenario-final" "tests/parity/$scenario.steps"
done
GTX_PERF_BIN="$work/gtx-parity-jobs" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-jobs-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
