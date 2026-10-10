#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/render/tabs.go internal/render/render.go internal/render/agent_ui_titles_test.go internal/tui/tui.go internal/tui/views.go internal/tui/mouse.go internal/tui/parity_tabbar_test.go)"
go build -o "$work/gtx-parity-tabbar" ./cmd/gtx
go vet ./...
go test -count=1 ./...
for scenario in tabbar-detail tabbar-arrows; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --gtx "$work/gtx-parity-tabbar" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-fixed"
 python3 tests/parity/region_compare.py "$work/parity-$scenario-fixed" --rect 26,0,134,1 --screens boot named eight-tabs --result wide-comparison.json
done
python3 tests/parity/region_compare.py "$work/parity-tabbar-detail-fixed" --rect 26,0,54,1 --screens overflow wheel-up wheel-down first tab-click --result narrow-comparison.json
python3 tests/parity/region_compare.py "$work/parity-tabbar-arrows-fixed" --rect 26,0,54,1 --screens overflow arrow-left arrow-left-again arrow-right select-first disabled-left first-arrow-right first-arrow-right-again --result narrow-comparison.json
for scenario in tabbar-buttons tabbar-middle; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --gtx "$work/gtx-parity-tabbar" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-fixed"
 python3 tests/parity/measure.py "$work/parity-$scenario-fixed"
done
python3 tests/parity/region_compare.py "$work/parity-tabbar-buttons-fixed" --rect 26,0,134,1 --screens after-enter middle-close --result bar-comparison.json
python3 tests/parity/region_compare.py "$work/parity-tabbar-middle-fixed" --rect 26,0,134,1 --screens inactive active active-again inactive-again --result bar-comparison.json
GTX_PERF_BIN="$work/gtx-parity-tabbar" GTX_PERF_BASELINE_BIN="$work/gtx-look-baseline-b0a18b0" GTX_PERF_OUT="$work/parity-tabbar-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
