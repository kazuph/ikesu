#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
git_files="internal/model/geometry.go internal/model/model_test.go internal/render/overlays.go internal/render/render.go internal/render/render_test.go internal/render/agent_ui_titles_test.go internal/render/sidebar_matrix_test.go internal/render/spec_contract_test.go internal/render/spec_frame_threshold_test.go internal/render/parity_chrome_test.go internal/render/parity_help_test.go internal/tui/tui.go internal/tui/loop.go internal/tui/copy.go internal/tui/parity_copy_test.go internal/tui/keys.go internal/tui/help.go internal/tui/mouse.go internal/tui/agent_ui_dialog_test.go internal/tui/spec_config_tab_test.go internal/tui/spec_metadata_test.go internal/tui/spec_help_mouse_test.go internal/tui/parity_help_test.go tools/bench/ptyrun/main.go"
test -z "$(gofmt -l $git_files)"
go build -o "$work/gtx-parity-input-final" ./cmd/gtx
go vet ./...
go test -count=1 ./...
for scenario in escape-repeat help-input paste copy-detail settings-detail; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --gtx "$work/gtx-parity-input-final" --ptyrun "$work/ptyrun-parity" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-input-final"
done
