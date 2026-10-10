#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/model/model.go internal/render/sidebar.go internal/render/sidebar_git.go internal/render/sidebar_viewport.go internal/render/sidebar_matrix_test.go internal/render/parity_sidebar_panel_test.go internal/tui/sidebar_mouse.go internal/tui/views.go)"
go build -o "$work/gtx-parity-sidebar-detail" ./cmd/gtx
go vet ./...
go test -count=1 ./...
python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --gtx "$work/gtx-parity-sidebar-detail" --ptyrun "$work/ptyrun-parity-cells" --steps tests/parity/sidebar-card-menu.steps --out "$work/parity-sidebar-cards-final"
python3 tests/parity/region_compare.py "$work/parity-sidebar-cards-final" --rect 0,0,25,23 --screens boot new-workspace first-active menu-closed slim full-again
python3 tests/parity/region_compare.py "$work/parity-sidebar-cards-final" --rect 0,0,4,23 --screens workspace-menu inactive-menu --result menu-card-comparison.json
python3 - "$work/parity-sidebar-cards-final" <<'PY'
import json
from pathlib import Path
import sys
rows = json.loads((Path(sys.argv[1])/'region-comparison.json').read_text())
expected = {'boot', 'new-workspace', 'first-active', 'menu-closed', 'slim', 'full-again'}
assert {row['screen'] for row in rows} == expected, rows
assert all(row['status'] == 'PASS' and row['difference_count'] == 0 for row in rows), rows
rows = json.loads((Path(sys.argv[1])/'menu-card-comparison.json').read_text())
assert {row['screen'] for row in rows} == {'workspace-menu', 'inactive-menu'}, rows
assert all(row['status'] == 'PASS' and row['difference_count'] == 0 for row in rows), rows
PY
for edge in top bottom; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --gtx "$work/gtx-parity-sidebar-detail" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/sidebar-panel-$edge.steps" --out "$work/parity-sidebar-$edge-final"
 python3 tests/parity/sidebar_panel_gate.py "$work/parity-sidebar-$edge-final" --edge "$edge"
done
GTX_PARITY_BIN="$work/gtx-parity-sidebar-detail" PARITY_FIXTURE_DIR="$PWD/tests/parity" PARITY_SIDEBAR_THEME_OUT="$work/parity-sidebar-theme-cards-final" sh tests/parity/probe-sidebar-themes.sh
python3 tests/parity/sidebar_theme_gate.py "$work/parity-sidebar-theme-cards-final"
GTX_PERF_BIN="$work/gtx-parity-sidebar-detail" GTX_PERF_BASELINE_BIN="$work/gtx-look-baseline-b0a18b0" GTX_PERF_OUT="$work/parity-sidebar-detail-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
