#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/render/render.go internal/render/overlays.go internal/render/help_lines.go internal/render/settings.go internal/tui/help.go internal/tui/help_test.go internal/tui/spec_focus_test.go internal/tui/spec_help_mouse_test.go)"
go build -o "$work/gtx-parity-help-detail" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
for scenario in help-complete help-mouse help-close help-size help-compact; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-help-detail" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-help-detail"
 python3 tests/parity/navigator_compare.py "$work/parity-$scenario-help-detail" --title 'keybinds' --result help-comparison.json
done
for scenario in help-size help-compact; do
 python3 tests/parity/settings_size_compare.py "$work/parity-$scenario-help-detail"
done
python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-help-detail" --ptyrun "$work/ptyrun-parity-cells" --steps tests/parity/overlay-background.steps --out "$work/parity-overlay-background-fixed"
python3 tests/parity/region_compare.py "$work/parity-overlay-background-fixed" --rect 26,0,134,1 --screens normal dialog dialog-closed help help-closed settings settings-closed navigator navigator-closed
python3 tests/parity/help_gate.py "$work"
IKESU_PERF_BIN="$work/gtx-parity-help-detail" IKESU_PERF_BASELINE_BIN="$work/gtx-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-help-detail-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
