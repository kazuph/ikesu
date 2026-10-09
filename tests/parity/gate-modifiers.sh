#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
test -z "$(gofmt -l internal cmd tests tools)"
export GTX_PARITY_BIN="$work/gtx-parity-modifiers-final"
go build -o "$GTX_PARITY_BIN" ./cmd/gtx
go vet ./...
go test -count=1 ./...
sh tests/parity/gate-modifier-section-cells.sh
python3 tests/parity/observe.py --server --server-reuse-initial --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps tests/parity/server-section-small-end.steps --out "$work/parity-section-small-end-final"
python3 tests/parity/full_frame_gate.py "$work/parity-section-small-end-final" tests/parity/server-section-small-end.steps
python3 tests/parity/menu_cases.py "$work/parity-main-menu-final-steps"
for steps in "$work"/parity-main-menu-final-steps/main-*.steps; do
 name=$(basename "$steps" .steps)
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$GTX_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps "$steps" --out "$work/parity-main-menu-final/$name"
 python3 tests/parity/full_frame_gate.py "$work/parity-main-menu-final/$name" "$steps"
done
GTX_PERF_BIN="$GTX_PARITY_BIN" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-modifiers-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
