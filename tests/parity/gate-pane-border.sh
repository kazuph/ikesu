#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/tui/tui.go internal/tui/mouse.go internal/tui/parity_pane_border_test.go)"
go build -o "$work/gtx-parity-pane-border" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
for scenario in pane-border pane-horizontal; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-pane-border" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-fixed"
 python3 tests/parity/measure.py "$work/parity-$scenario-fixed"
done
IKESU_PERF_BIN="$work/gtx-parity-pane-border" IKESU_PERF_BASELINE_BIN="$work/gtx-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-pane-border-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
