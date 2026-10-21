#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/look"
work="$HOME/bench-terrace.noindex/work"
if ! test -x "$work/herdr-0.2.24"; then
 echo "NOT_RUN: herdr distribution binary is absent"
 exit 0
fi
test -z "$(gofmt -l internal/render/parity_modes.go internal/tui/keys.go internal/tui/menus.go internal/tui/parity_tab_rename_test.go)"
go build -o "$work/gtx-parity-prefix-style" ./cmd/ikesu
go vet ./...
go test -count=1 ./...
python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-prefix-style" --ptyrun "$work/ptyrun-parity-cells" --steps tests/parity/tab-rename-target.steps --out "$work/parity-tab-rename-final"
python3 tests/parity/region_compare.py "$work/parity-tab-rename-final" --rect 52,19,56,7 --screens rename reopened
python3 tests/parity/region_compare.py "$work/parity-tab-rename-final" --rect 0,0,160,45 --screens before saved cancelled --result full-comparison.json
python3 tests/parity/prefix_cases.py "$work/parity-prefix-style-steps"
for case in cancel resize workspace-nav rename-tab; do
 python3 tests/parity/observe.py --clipboard-sandbox --herdr "$work/herdr-0.2.24" --ikesu "$work/gtx-parity-prefix-style" --ptyrun "$work/ptyrun-parity-cells" --steps "$work/parity-prefix-style-steps/$case.steps" --out "$work/parity-prefix-style-final/$case"
 python3 tests/parity/region_compare.py "$work/parity-prefix-style-final/$case" --rect 0,0,160,45 --screens pending result
 done
python3 - "$work" <<'PY'
import json
from pathlib import Path
import sys
work = Path(sys.argv[1])
paths = [work/'parity-tab-rename-final'/name for name in ('region-comparison.json', 'full-comparison.json')]
paths += [work/'parity-prefix-style-final'/case/'region-comparison.json' for case in ('cancel','resize','workspace-nav','rename-tab')]
for path in paths:
    rows = json.loads(path.read_text())
    assert rows and all(row['status']=='PASS' and row['difference_count']==0 for row in rows), path
PY
IKESU_PERF_BIN="$work/gtx-parity-prefix-style" IKESU_PERF_BASELINE_BIN="$work/gtx-look-baseline-b0a18b0" IKESU_PERF_OUT="$work/parity-prefix-style-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
