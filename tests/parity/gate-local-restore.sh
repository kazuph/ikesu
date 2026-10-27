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
go build -o "$work/ikesu-parity-local-restore" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
for scenario in close-workspace-detail close-workspace-name restore-confirm navigate-prefix-c navigate-prefix-v navigate-direct-c navigate-direct-v; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-local-restore" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/$scenario.steps" --out "$work/parity-$scenario-local-final"
 python3 tests/parity/full_frame_gate.py "$work/parity-$scenario-local-final" "tests/parity/$scenario.steps"
done
python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-local-restore" --ptyrun "$work/ptyrun-parity-cells" --steps tests/parity/worktree-reject.steps --out "$work/parity-worktree-local-final"
python3 tests/parity/worktree_warning_gate.py "$work/parity-worktree-local-final"
python3 tests/parity/prefix_cases.py "$work/parity-local-prefix-steps"
for case in close-pane close-tab close-workspace reload workspace-nav; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/ikesu-parity-local-restore" --ptyrun "$work/ptyrun-parity-cells" --steps "$work/parity-local-prefix-steps/$case.steps" --out "$work/parity-local-prefix-final/$case"
 python3 tests/parity/full_frame_gate.py "$work/parity-local-prefix-final/$case" "$work/parity-local-prefix-steps/$case.steps"
done
IKESU_PERF_BIN="$work/ikesu-parity-local-restore" IKESU_PERF_BASELINE_BIN="$work/ikesu-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-local-restore-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
