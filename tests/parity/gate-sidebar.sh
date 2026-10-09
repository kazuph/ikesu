#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-target/release/herdr"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/config/config.go internal/config/sidebar.go internal/model/geometry.go internal/render/jobs.go internal/render/mobile.go internal/render/render_test.go internal/tui/spec_metadata_test.go internal/render/sidebar.go internal/render/sidebar_tokens.go internal/render/render.go internal/tui/mouse.go internal/tui/sidebar.go internal/tui/sidebar_mouse.go internal/tui/sidebar_scroll.go internal/tui/sidebar_spec_test.go internal/tui/parity_sidebar_test.go)"
go build -o "$work/gtx-parity-sidebar" ./cmd/gtx
go vet ./...
go test -count=1 ./...
for scenario in sidebar-controls sidebar-width sidebar-collapsed sidebar-drag mobile-layout; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-sidebar" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-sidebar"
done
python3 tests/parity/region_compare.py "$work/parity-sidebar-collapsed-sidebar" --rect 0,0,4,45 --screens three-collapsed first-workspace active-workspace second-workspace --result collapsed-comparison.json
python3 tests/parity/region_compare.py "$work/parity-sidebar-collapsed-sidebar" --rect 0,0,60,2 --screens mobile mobile-escape --result mobile-header-comparison.json
GTX_PERF_BIN="$work/gtx-parity-sidebar" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-sidebar-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
