#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
gofmt -w internal/tui/navigator.go internal/tui/parity_navigator_test.go
go build -o "$work/gtx-parity-navigator-selection" ./cmd/gtx
go vet ./...
go test ./...
GTX_PARITY_BIN="$work/gtx-parity-navigator-selection" sh tests/parity/probe-navigator.sh
for scenario in navigator-matrix navigator-header navigator-gutter; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-navigator-selection" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-selection"
 python3 tests/parity/navigator_compare.py "$work/parity-$scenario-selection"
done
GTX_PERF_BIN="$work/gtx-parity-navigator-selection" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-navigator-selection-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
