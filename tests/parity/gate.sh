#!/bin/sh
set -eu
cd "$HOME/bench-terrace.noindex/src/parity"
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
work="$HOME/bench-terrace.noindex/work"
test -z "$(gofmt -l internal/tui/menus.go internal/tui/spec_help_mouse_test.go tools/bench/ptyrun/main.go)"
go build -o "$work/gtx-parity" ./cmd/gtx
go vet ./...
go test ./...
(cd tools/bench/ptyrun && go build -o "$work/ptyrun-parity" .)
for scenario in menu menu-routing overlays; do
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity" --ptyrun "$work/ptyrun-parity" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-after"
done
