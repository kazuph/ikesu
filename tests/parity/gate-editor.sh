#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/tui/edit_scrollback.go internal/tui/tui.go internal/tui/loop.go internal/tui/parity_editor_test.go)"
go build -o "$work/ikesu-parity-editor" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
for scenario in editor-scrollback editor-layout; do
 EDITOR=/usr/bin/vi python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-editor" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-fixed"
 python3 tests/parity/measure.py "$work/parity-$scenario-fixed"
done
EDITOR=/usr/bin/vi VISUAL= python3 tests/parity/observe.py --server --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-editor" --ptyrun "$work/ptyrun-parity-cells" --steps tests/parity/popup-editor.steps --out "$work/parity-popup-editor-fixed"
IKESU_PERF_BIN="$work/ikesu-parity-editor" IKESU_PERF_BASELINE_BIN="$work/ikesu-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-editor-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
