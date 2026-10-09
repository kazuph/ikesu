#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-target/release/herdr"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
: "${PARITY_CONTROLLER_SESSION_ID:?set the validated controller transcript session ID}"
export PARITY_CONTROLLER_SESSION_ID
test -z "$(gofmt -l internal cmd tests tools)"
export GTX_PARITY_BIN="$work/gtx-parity-final"
go build -o "$GTX_PARITY_BIN" ./cmd/gtx
go build -o "$work/ptyrun-jobs-final" tools/bench/ptyrun/main.go
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
for mode in local server; do
 if test "$mode" = server; then
  set -- --server --server-reuse-initial
 else
  set --
 fi
 for scenario in mobile-navigator desktop-navigator-backdrop close-workspace-confirmed close-workspace-detail close-workspace-name navigate-prefix-c navigate-prefix-v navigate-direct-c navigate-direct-v; do
  python3 tests/parity/observe.py "$@" --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-$mode-final"
  python3 tests/parity/full_frame_gate.py "$work/parity-$scenario-$mode-final" "tests/parity/$scenario.steps"
 done
done
python3 tests/parity/observe.py --server --server-reuse-initial --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps tests/parity/new-workspace-agent-name.steps --out "$work/parity-new-workspace-agent-name-final"
python3 tests/parity/full_frame_gate.py "$work/parity-new-workspace-agent-name-final" tests/parity/new-workspace-agent-name.steps
sh tests/parity/gate-modifier-section-cells.sh
python3 tests/parity/prefix_cases.py "$work/parity-last-prefix-steps"
for name in new-workspace close-pane close-tab; do
 steps="$work/parity-last-prefix-steps/$name.steps"
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps "$steps" --out "$work/parity-last-prefix/$name"
 python3 tests/parity/full_frame_gate.py "$work/parity-last-prefix/$name" "$steps"
done
for mode in target blocked; do
 steps="tests/parity/prefix-notification-$mode-settled.steps"
 out="$work/parity-prefix-notification-$mode-final-gate"
 python3 tests/parity/observe.py --cell-clock --server --server-reuse-initial --in-app-toast --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps "$steps" --out "$out"
 python3 tests/parity/notification_target_gate.py "$out"
 if test "$mode" = target; then
  kind=finished
 else
  kind=blocked
 fi
 python3 tests/parity/notification_frame_gate.py "$out" "$kind"
done
python3 tests/parity/observe.py --cell-clock --server --server-reuse-initial --in-app-toast --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps tests/parity/prefix-notification-expiry.steps --out "$work/parity-notification-expiry-final-gate"
python3 tests/parity/notification_clock_gate.py "$work/parity-notification-expiry-final-gate"
(cd "$HOME/bench-terrace.noindex/src/parity-baseline-9347b07" && go build -o "$work/gtx-parity-baseline-9347b07" ./cmd/gtx)
GTX_PERF_BIN="$GTX_PARITY_BIN" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-final-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
