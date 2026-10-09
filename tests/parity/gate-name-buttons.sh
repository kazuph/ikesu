#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
gofmt -w internal/render/overlays.go internal/tui/menus.go internal/tui/parity_name_buttons_test.go
go build -o "$work/gtx-parity-name-buttons" ./cmd/gtx
go vet ./...
go test ./...
for action in save clear cancel; do
 out="$work/parity-name-button-$action-final"
 python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-name-buttons" --ptyrun "$work/ptyrun-parity-cells" --steps "tests/parity/name-button-$action.steps" --out "$out"
 python3 tests/parity/region_compare.py "$out" --rect 52,19,56,7 --screens open
 if [ "$action" = clear ]; then
  python3 tests/parity/region_compare.py "$out" --rect 52,19,56,7 --screens after --result after-comparison.json
 else
  python3 tests/parity/region_compare.py "$out" --rect 26,0,134,1 --screens after --result after-comparison.json
 fi
 python3 - "$out" <<'PY'
import json
from pathlib import Path
import sys
root = Path(sys.argv[1])
for filename in ('region-comparison.json', 'after-comparison.json'):
    rows = json.loads((root/filename).read_text())
    assert len(rows) == 1 and rows[0]['status'] == 'PASS' and rows[0]['difference_count'] == 0, rows
PY
done
GTX_PERF_BIN="$work/gtx-parity-name-buttons" GTX_PERF_BASELINE_BIN="$work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$work/parity-name-buttons-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
