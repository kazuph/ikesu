#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-target/release/herdr"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/render/mobile.go internal/render/mobile_test.go internal/render/render.go internal/render/sidebar.go internal/tui/mobile.go internal/tui/mobile_test.go internal/tui/menus.go internal/tui/keys.go internal/tui/views.go internal/tui/sidebar.go internal/tui/parity_name_buttons_test.go)"
go build -o "$work/gtx-parity-mobile" ./cmd/gtx
go vet ./...
go test -count=1 ./...
for scenario in mobile-switcher mobile-git; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-mobile" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-fixed"
done
python3 tests/parity/region_compare.py "$work/parity-mobile-switcher-fixed" --rect 0,0,60,25 --screens open wheel-down bottom wheel-up reopen
python3 tests/parity/region_compare.py "$work/parity-mobile-switcher-fixed" --rect 0,0,60,45 --screens tall --result tall-comparison.json
python3 tests/parity/region_compare.py "$work/parity-mobile-git-fixed" --rect 0,0,60,45 --screens git-switcher
python3 - "$work" <<'PY'
import json
from pathlib import Path
import sys
root = Path(sys.argv[1])
for directory, filename, screens in (
    ('parity-mobile-switcher-fixed', 'region-comparison.json', {'open', 'wheel-down', 'bottom', 'wheel-up', 'reopen'}),
    ('parity-mobile-switcher-fixed', 'tall-comparison.json', {'tall'}),
    ('parity-mobile-git-fixed', 'region-comparison.json', {'git-switcher'}),
):
    rows = json.loads((root/directory/filename).read_text())
    assert {row['screen'] for row in rows} == screens, rows
    assert all(row['status'] == 'PASS' and row['difference_count'] == 0 for row in rows), rows
PY
GTX_PARITY_BIN="$work/gtx-parity-mobile" sh tests/parity/probe-mobile.sh
python3 tests/parity/mobile_compare.py "$work/parity-mobile-actions"
GTX_PERF_BIN="$work/gtx-parity-mobile" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-mobile-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
