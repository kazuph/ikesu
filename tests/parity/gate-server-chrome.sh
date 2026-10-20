#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal cmd tests tools)"
go build -o "$work/gtx-parity-server-chrome" ./cmd/ikesu
go build -o "$work/ptyrun-server-chrome" tools/bench/ptyrun/main.go
go vet ./...
go test -count=1 ./...
for scenario in server-chrome-detail server-sidebar-controls server-tab-name; do
 out="$work/parity-$scenario-final"
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-server-chrome" --ptyrun "$work/ptyrun-server-chrome" --server --server-reuse-initial --steps "tests/parity/$scenario.steps" --out "$out"
 python3 tests/parity/full_frame_gate.py "$out" "tests/parity/$scenario.steps"
done
python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-server-chrome" --ptyrun "$work/ptyrun-server-chrome" --steps tests/parity/server-tab-name.steps --out "$work/parity-local-tab-name-final"
python3 tests/parity/full_frame_gate.py "$work/parity-local-tab-name-final" tests/parity/server-tab-name.steps
python3 tests/parity/server_tab_cases.py "$work/parity-server-tab-steps"
python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-server-chrome" --ptyrun "$work/ptyrun-server-chrome" --server --server-reuse-initial --steps "$work/parity-server-tab-steps/overflow.steps" --out "$work/parity-server-tabs-final"
python3 tests/parity/full_frame_gate.py "$work/parity-server-tabs-final" "$work/parity-server-tab-steps/overflow.steps"
python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-server-chrome" --ptyrun "$work/ptyrun-server-chrome" --server --server-reuse-initial --steps tests/parity/server-main-menu.steps --out "$work/parity-server-main-final"
python3 tests/parity/main_menu_frame_gate.py "$work/parity-server-main-final"
python3 tests/parity/observe.py --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-server-chrome" --ptyrun "$work/ptyrun-server-chrome" --server --server-reuse-initial --clipboard-sandbox --steps tests/parity/server-copy-controls.steps --out "$work/parity-server-copy-final"
python3 tests/parity/full_frame_gate.py "$work/parity-server-copy-final" tests/parity/server-copy-controls.steps
python3 tests/parity/osc52_gate.py "$work/parity-server-copy-final"
IKESU_PERF_BIN="$work/gtx-parity-server-chrome" IKESU_PERF_BASELINE_BIN="$work/gtx-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-server-chrome-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
