#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
gofmt -w internal/tui/menus.go internal/tui/mouse.go internal/tui/parity_menu_test.go internal/render/overlays.go internal/render/agent_ui_contract_test.go
go build -o "$work/gtx-parity-menu-selection" ./cmd/gtx
go vet ./...
go test ./...
for scenario in menu-initial menu-select menus; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-menu-selection" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-selection"
done
python3 tests/parity/cell_styles.py "$work/parity-menu-initial-selection" pane-open 'Split vertical'
