#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/render/overlays.go internal/render/render.go internal/render/parity_modes.go internal/render/parity_confirm_test.go internal/render/parity_menu_style_test.go internal/render/render_test.go internal/render/agent_ui_contract_test.go internal/render/agent_ui_titles_test.go internal/tui/tui.go internal/tui/keys.go internal/tui/parity_modes.go internal/tui/menus.go internal/tui/sidebar.go internal/tui/agent_ui_dialog_test.go internal/tui/parity_name_buttons_test.go)"
go build -o "$work/ikesu-parity-overlays" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
for scenario in confirm-cells confirm-navigation workspace-rename-initial main-menu-style copy-cycle copy-page-sizes; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-overlays" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-final"
done
python3 tests/parity/confirm_cells_gate.py "$work/parity-confirm-cells-final"
python3 tests/parity/confirm_cells_gate.py "$work/parity-confirm-navigation-final" --navigation
for scenario in copy-cycle copy-page-sizes; do
 python3 tests/parity/actionbar_gate.py "$work/parity-$scenario-final" "tests/parity/$scenario.steps"
done
python3 tests/parity/region_compare.py "$work/parity-workspace-rename-initial-final" --rect 52,19,56,7 --screens initial typed reopened
python3 tests/parity/region_compare.py "$work/parity-workspace-rename-initial-final" --rect 0,0,25,23 --screens cancelled --result cancelled-comparison.json
python3 - "$work/parity-workspace-rename-initial-final" <<'PY'
import json
from pathlib import Path
import sys
root = Path(sys.argv[1])
for filename, count in [('region-comparison.json', 3), ('cancelled-comparison.json', 1)]:
    rows = json.loads((root/filename).read_text())
    assert len(rows) == count and all(row['status'] == 'PASS' and row['difference_count'] == 0 for row in rows), rows
PY
# Menu-item differences are observations, not passes for this confirmation/name gate.
python3 tests/parity/navigator_compare.py "$work/parity-main-menu-style-final" --title ' new workspace' --result main-menu-comparison.json
IKESU_PERF_BIN="$work/ikesu-parity-overlays" IKESU_PERF_BASELINE_BIN="$work/ikesu-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-overlays-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
