#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
gofmt -w internal/tui/settings.go internal/tui/settings_test.go internal/tui/parity_settings_test.go internal/render/settings.go
go build -o "$work/gtx-parity-settings" ./cmd/gtx
go vet ./...
go test ./...
for scenario in settings-detail settings-preview settings-pending; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-settings" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-settings"
 python3 tests/parity/navigator_compare.py "$work/parity-$scenario-settings" --title ' settings' --result settings-comparison.json
done
GTX_PERF_BIN="$work/gtx-parity-settings" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-settings-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
