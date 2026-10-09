#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
test -z "$(gofmt -l internal cmd tests tools)"
go build -o "$work/gtx-parity-copy-success" ./cmd/gtx
go build -o "$work/ptyrun-copy-success" tools/bench/ptyrun/main.go
go vet ./...
go test -count=1 ./...
for scenario in copy-guard-smoke copy-mouse-release copy-wide-cursor; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-copy-success" --ptyrun "$work/ptyrun-copy-success" --steps "tests/parity/$scenario.steps" --clipboard-sandbox --out "$work/parity-$scenario-success-final"
 if test "$scenario" = copy-wide-cursor; then
  python3 tests/parity/osc52_gate.py "$work/parity-$scenario-success-final" --expect-empty
 else
  python3 tests/parity/osc52_gate.py "$work/parity-$scenario-success-final"
 fi
 python3 tests/parity/full_frame_gate.py "$work/parity-$scenario-success-final" "tests/parity/$scenario.steps"
done
python3 tests/parity/copy_success_cases.py "$work/parity-copy-success-steps"
for scenario in multiple-y multiple-enter wide-y backward-y fullscreen-y no-anchor-enter; do
 out="$work/parity-copy-success-final/$scenario"
 steps="$work/parity-copy-success-steps/$scenario.steps"
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-copy-success" --ptyrun "$work/ptyrun-copy-success" --steps "$steps" --clipboard-sandbox --out "$out"
 if test "$scenario" = no-anchor-enter; then
  python3 tests/parity/osc52_gate.py "$out" --expect-empty
 else
  python3 tests/parity/osc52_gate.py "$out"
 fi
 python3 tests/parity/full_frame_gate.py "$out" "$steps"
done
GTX_PERF_BIN="$work/gtx-parity-copy-success" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-copy-success-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
