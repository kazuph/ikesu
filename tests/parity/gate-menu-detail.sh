#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
gofmt -w internal/tui/menus.go internal/tui/parity_menu_test.go internal/tui/views.go
go build -o "$work/gtx-parity-menu-detail" ./cmd/gtx
go vet ./...
go test ./...
for scenario in menu-bottom menu-initial menu-resize; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-menu-detail" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-menu-detail"
done
python3 tests/parity/menu_boundary_gate.py "$work"
GTX_PERF_BIN="$work/gtx-parity-menu-detail" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-menu-detail-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
