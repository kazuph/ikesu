#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/tui/menus.go internal/tui/parity_menu_test.go internal/tui/views.go)"
go build -o "$work/ikesu-parity-menu-detail" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
for scenario in menu-bottom menu-initial menu-resize; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-menu-detail" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-menu-detail"
done
python3 tests/parity/menu_boundary_gate.py "$work"
IKESU_PERF_BIN="$work/ikesu-parity-menu-detail" IKESU_PERF_BASELINE_BIN="$work/ikesu-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-menu-detail-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
