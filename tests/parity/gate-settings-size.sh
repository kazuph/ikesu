#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/render/settings.go internal/tui/settings.go)"
go build -o "$work/ikesu-parity-settings-size" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
for scenario in settings-size settings-height-sections settings-detail settings-preview settings-pending; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-settings-size" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-size-fixed"
done
for scenario in settings-size settings-height-sections; do
 python3 tests/parity/settings_size_compare.py "$work/parity-$scenario-size-fixed"
done
python3 tests/parity/settings_gate.py "$work"
IKESU_PERF_BIN="$work/ikesu-parity-settings-size" IKESU_PERF_BASELINE_BIN="$work/ikesu-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-settings-size-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
