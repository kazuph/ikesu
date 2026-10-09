#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-target/release/herdr"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l third_party/vt/emulator.go third_party/vt/screen.go internal/runtime/parity_resize_test.go internal/tui/mouse.go internal/tui/spec_mouse_regions_test.go internal/copybuf/copybuf.go internal/tui/parity_copy_wide_test.go internal/render/render.go internal/tui/copy.go internal/tui/views.go internal/tui/spec_copy_navigation_test.go internal/tui/parity_copy_test.go internal/runtime/scrollback_test.go third_party/vt/scrollback.go third_party/vt/scrollback_ring_test.go internal/tui/loop.go internal/tui/spec_layout_runtime_test.go internal/render/spec_contract_test.go tests/e2e/e2e_test.go tests/e2e/parity_tabs_test.go)"
go build -o "$work/gtx-parity-copy-safe" ./cmd/gtx
(cd third_party/vt && go vet ./... && go test -count=1 ./...)
go vet ./...
go test -count=1 ./...
for scenario in copy-clean-cells copy-key-details copy-selection-keys copy-page-sizes copy-wheel-cursor copy-unicode copy-cycle copy-colored-selection copy-wide-motions copy-resize-top copy-resize-middle copy-resize-bottom; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-copy-safe" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-final"
 python3 tests/parity/copy_cells_gate.py "$work/parity-$scenario-final" "tests/parity/$scenario.steps"
done
GTX_PERF_BIN="$work/gtx-parity-copy-safe" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-copy-safe-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
