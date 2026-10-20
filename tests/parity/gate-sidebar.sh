#!/bin/sh
# Run on mini. Preserve the shared/mobile gates; replace only sidebar visuals.
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
out="${1:?usage: gate-sidebar.sh OUTPUT BASELINE_BFDB572_ARCHIVE}"
archive="${2:?baseline bfdb572 git archive is required}"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
work="$HOME/bench-terrace.noindex/work"
herdr="${IKESU_PARITY_HERDR:-${GTX_PARITY_HERDR:-$work/herdr-0.2.24}}"
ptyrun="${IKESU_PARITY_PTYRUN:-${GTX_PARITY_PTYRUN:-$work/ptyrun-parity-cells}}"
test -x "$herdr"
test -x "$ptyrun"
gofmt -l . > "$out/gofmt.log"
test ! -s "$out/gofmt.log"
go build -trimpath '-ldflags=-s -w' -o "$out/gtx" ./cmd/ikesu > "$out/build.log" 2>&1
go vet ./... > "$out/vet.log" 2>&1
for scenario in sidebar-controls sidebar-width sidebar-collapsed sidebar-drag mobile-layout; do
 set -- --ikesu-steps "tests/parity/right-$scenario.steps"
 python3 tests/parity/observe.py "$@" --silent --clipboard-sandbox --herdr "$herdr" --ikesu "$out/gtx" --ptyrun "$ptyrun" --steps "tests/parity/$scenario.steps" --out "$out/$scenario" > "$out/$scenario.log" 2>&1
done
python3 tests/parity/region_compare.py "$out/sidebar-collapsed" --rect 0,0,60,2 --screens mobile mobile-escape --result mobile-header-comparison.json
python3 - "$out/sidebar-collapsed/mobile-header-comparison.json" <<'PY'
import json,sys
rows=json.load(open(sys.argv[1]))
assert len(rows)==2 and all(r['status']=='PASS' and r['difference_count']==0 for r in rows),rows
PY
GTX_SIDEBAR_EVIDENCE="$out/screens" go test -p 1 -v -count=1 -timeout 20m ./... > "$out/test.log" 2>&1
go test -race -p 1 -v -count=1 -timeout 20m ./... > "$out/race.log" 2>&1
(cd third_party/vt && go test -count=1 ./... && go test -race -count=1 ./...) > "$out/vt.log" 2>&1
python3 tests/parity/right_sidebar_gate.py "$out/screens"
python3 tests/parity/right_sidebar_perf.py "$archive" "$out/perf"
