#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
gofmt -w internal/tui/tui.go internal/tui/mouse.go internal/tui/parity_pane_border_test.go
go build -o "$work/gtx-parity-pane-border" ./cmd/gtx
go vet ./...
go test ./...
for scenario in pane-border pane-horizontal; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-pane-border" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-fixed"
 python3 tests/parity/measure.py "$work/parity-$scenario-fixed"
done
GTX_PERF_BIN="$work/gtx-parity-pane-border" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-pane-border-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
