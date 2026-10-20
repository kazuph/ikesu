#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/tui/settings.go internal/tui/settings_test.go internal/tui/parity_settings_test.go internal/render/settings.go)"
go build -o "$work/gtx-parity-settings" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
for scenario in settings-detail settings-preview settings-pending; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-settings" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-settings"
 python3 tests/parity/navigator_compare.py "$work/parity-$scenario-settings" --title ' settings' --result settings-comparison.json
done
IKESU_PERF_BIN="$work/gtx-parity-settings" IKESU_PERF_BASELINE_BIN="$work/gtx-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-settings-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
