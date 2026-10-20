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
export IKESU_PARITY_BIN="$work/gtx-parity-modifiers-final"
go build -o "$IKESU_PARITY_BIN" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
sh tests/parity/gate-modifier-section-cells.sh
python3 tests/parity/observe.py --server --server-reuse-initial --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$IKESU_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps tests/parity/server-section-small-end.steps --out "$work/parity-section-small-end-final"
python3 tests/parity/full_frame_gate.py "$work/parity-section-small-end-final" tests/parity/server-section-small-end.steps
python3 tests/parity/menu_cases.py "$work/parity-main-menu-final-steps"
for steps in "$work"/parity-main-menu-final-steps/main-*.steps; do
 name=$(basename "$steps" .steps)
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$IKESU_PARITY_BIN" --ptyrun "$work/ptyrun-jobs-final" --steps "$steps" --out "$work/parity-main-menu-final/$name"
 python3 tests/parity/full_frame_gate.py "$work/parity-main-menu-final/$name" "$steps"
done
IKESU_PERF_BIN="$IKESU_PARITY_BIN" IKESU_PERF_BASELINE_BIN="$work/gtx-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-modifiers-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
