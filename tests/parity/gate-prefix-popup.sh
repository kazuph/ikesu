#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
gofmt -w internal/server/server_test.go internal/tui/spec_focus_test.go internal/tui/parity_prefix_test.go internal/config/config.go internal/config/config_test.go internal/render/render.go internal/tui/help.go internal/tui/help_test.go internal/tui/keys.go internal/tui/mouse.go internal/tui/popup.go internal/tui/parity_popup_test.go internal/tui/spec_copy_docs_test.go internal/tui/spec_layout_runtime_test.go internal/tui/tui.go internal/tui/parity_modes.go internal/tui/views.go
go build -o "$work/gtx-parity-prefix-popup" ./cmd/gtx
go vet ./...
go test ./...
for scenario in prefix-routing resize-detail help-input sidebar-detail menus menu-select; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-prefix-popup" --ptyrun "$work/ptyrun-parity" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-prefix-popup"
done
for scenario in popup popup-detail; do
 python3 tests/parity/observe.py --server --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-prefix-popup" --ptyrun "$work/ptyrun-parity" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-prefix-popup"
done
