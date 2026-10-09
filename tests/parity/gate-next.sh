#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
gofmt -w internal/render/sidebar_matrix_test.go internal/render/spec_contract_test.go internal/render/spec_frame_threshold_test.go internal/tui/spec_config_tab_test.go internal/tui/spec_metadata_test.go internal/tui/loop.go internal/tui/agent_ui_dialog_test.go internal/render/agent_ui_titles_test.go tools/bench/ptyrun/main.go internal/model/geometry.go internal/model/model_test.go internal/render/overlays.go internal/render/render.go internal/render/render_test.go internal/render/parity_help_test.go internal/render/parity_chrome_test.go internal/tui/help.go internal/tui/keys.go internal/tui/mouse.go internal/tui/spec_help_mouse_test.go internal/tui/parity_help_test.go
go build -o "$work/gtx-parity-next" ./cmd/gtx
go vet ./...
go test ./...
(cd tools/bench/ptyrun && go build -o "$work/ptyrun-parity" .)
for scenario in help-input menus-dialog menu-select paste; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-next" --ptyrun "$work/ptyrun-parity" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-next"
done
python3 tests/parity/observe.py --server --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-next" --ptyrun "$work/ptyrun-parity" --steps tests/parity/popup.steps --out "$work/parity-popup-next"
