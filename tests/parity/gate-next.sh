#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/render/sidebar_matrix_test.go internal/render/spec_contract_test.go internal/render/spec_frame_threshold_test.go internal/tui/spec_config_tab_test.go internal/tui/spec_metadata_test.go internal/tui/loop.go internal/tui/agent_ui_dialog_test.go internal/render/agent_ui_titles_test.go tools/bench/ptyrun/main.go internal/model/geometry.go internal/model/model_test.go internal/render/overlays.go internal/render/render.go internal/render/render_test.go internal/render/parity_help_test.go internal/render/parity_chrome_test.go internal/tui/help.go internal/tui/keys.go internal/tui/mouse.go internal/tui/spec_help_mouse_test.go internal/tui/parity_help_test.go)"
go build -o "$work/ikesu-parity-next" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
(cd tools/bench/ptyrun && go build -o "$work/ptyrun-parity" .)
for scenario in help-input menus-dialog menu-select paste; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-next" --ptyrun "$work/ptyrun-parity" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-next"
done
python3 tests/parity/observe.py --server --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-next" --ptyrun "$work/ptyrun-parity" --steps tests/parity/popup.steps --out "$work/parity-popup-next"
