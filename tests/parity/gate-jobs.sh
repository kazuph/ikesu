#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
: "${PARITY_CONTROLLER_SESSION_ID:?set the actual controller session ID}"
export PARITY_CONTROLLER_SESSION_ID
test -z "$(gofmt -l internal cmd tests tools)"
go build -o "$work/ikesu-parity-jobs" ./cmd/ikesu
go build -o "$work/ptyrun-parity-jobs" tools/bench/ptyrun/main.go
go vet ./...
go test -count=1 ./...
for scenario in jobs-panel jobs-success jobs-log-keys jobs-log-idle jobs-fine jobs-cancel jobs-cancelling; do
 python3 tests/parity/observe.py --server --server-reuse-initial --cell-clock --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-jobs" --ptyrun "$work/ptyrun-parity-jobs" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-final"
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
 python3 tests/parity/observe.py --server --server-reuse-initial --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-jobs" --ptyrun "$work/ptyrun-parity-jobs" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-final"
 python3 tests/parity/full_frame_gate.py "$work/parity-$scenario-final" "tests/parity/$scenario.steps"
done
IKESU_PERF_BIN="$work/ikesu-parity-jobs" IKESU_PERF_BASELINE_BIN="$work/ikesu-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-jobs-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
