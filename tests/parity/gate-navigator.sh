#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/tui/navigator.go internal/tui/parity_navigator_test.go internal/render/navigator.go internal/tui/keys.go internal/tui/mouse.go internal/tui/loop.go internal/tui/tui.go internal/tui/views.go internal/tui/parity_modes.go internal/render/render.go)"
go build -o "$work/ikesu-parity-navigator" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
for scenario in navigate-detail navigator-matrix navigator-tree navigator-long navigator-header navigator-gutter; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-navigator" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-navigator"
done

for scenario in navigate-detail navigator-matrix navigator-long navigator-header navigator-gutter; do
 python3 tests/parity/navigator_compare.py "$work/parity-$scenario-navigator"
done
IKESU_PERF_BIN="$work/ikesu-parity-navigator" IKESU_PERF_BASELINE_BIN="$work/ikesu-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-navigator-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
