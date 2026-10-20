#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/tui/menus.go internal/tui/parity_menu_test.go internal/tui/views.go internal/render/navigator.go internal/tui/parity_navigator_test.go)"
go build -o "$work/gtx-parity-detail" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
for scenario in navigate-detail navigator-matrix help-complete settings-detail chrome; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-detail" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-detail"
done
