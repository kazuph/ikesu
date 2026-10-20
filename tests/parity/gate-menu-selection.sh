#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/tui/menus.go internal/tui/mouse.go internal/tui/parity_menu_test.go internal/render/overlays.go internal/render/agent_ui_contract_test.go)"
go build -o "$work/gtx-parity-menu-selection" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
for scenario in menu-initial menu-select menus; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-menu-selection" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-selection"
done
python3 tests/parity/cell_styles.py "$work/parity-menu-initial-selection" pane-open 'Split vertical'
