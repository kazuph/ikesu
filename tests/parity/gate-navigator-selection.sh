#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/tui/navigator.go internal/tui/parity_navigator_test.go)"
go build -o "$work/gtx-parity-navigator-selection" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
IKESU_PARITY_BIN="$work/gtx-parity-navigator-selection" sh tests/parity/probe-navigator.sh
for scenario in navigator-matrix navigator-header navigator-gutter; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-navigator-selection" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-selection"
 python3 tests/parity/navigator_compare.py "$work/parity-$scenario-selection"
done
IKESU_PERF_BIN="$work/gtx-parity-navigator-selection" IKESU_PERF_BASELINE_BIN="$work/gtx-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-navigator-selection-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
