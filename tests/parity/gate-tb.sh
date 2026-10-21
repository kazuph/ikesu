#!/bin/sh
# Run on mini through work/perf-lock.py so builds and tests cannot disturb P1.
set -eu
phase="${2:-all}"
case "$phase" in
    all|validation) ;;
    *) printf 'expected phase all or validation\n' >&2; exit 1 ;;
esac
root="$HOME/bench-terrace.noindex"
export PATH="$root/tools/go/bin:$PATH"
export GOMODCACHE="$root/gomodcache"
cd "$root/src/tb-optimize"
out="$root/work/tb-optimize/${1:?evidence directory name required}"
mkdir -p "$out"
gofmt -l . > "$out/gofmt.log"
test ! -s "$out/gofmt.log"
go vet ./... > "$out/vet.log" 2>&1
go test -count=1 ./... > "$out/test.log" 2>&1
go test -race -count=1 ./... > "$out/race.log" 2>&1
go build -o "$out/gtx" ./cmd/ikesu
for scenario in graphics reuse; do
    python3 tests/parity/observe.py \
        --herdr "$root/work/detail/herdr-0.2.57" --ikesu "$out/gtx" \
        --ptyrun "$root/work/detail/ptyrun" \
        --config-file "$root/work/tb-optimize/config.toml" \
        --steps "tests/parity/tb-$scenario.steps" --out "$out/$scenario" \
        --server --cell-width 8 --cell-height 16
    if [ "$scenario" = reuse ]; then
        python3 tests/parity/tb_graphics_gate.py "$out/$scenario" --reuse
    else
        python3 tests/parity/tb_graphics_gate.py "$out/$scenario"
    fi
done
if [ "$phase" = validation ]; then
    printf 'PASS: gofmt, vet, test, race, T7/T10/T17 observations; P1 must be run separately\n'
    exit 0
fi
IKESU_PERF_BIN="$out/gtx" \
IKESU_PERF_BASELINE_BIN="$root/work/detail/gtx-baseline-06614cc" \
IKESU_PERF_OUT="$out/relative-perf.md" \
go test -tags perf -count=1 -timeout=30m -run TestRelative -v ./tests/perf > "$out/perf.log" 2>&1
printf 'PASS: gofmt, vet, test, race, T7/T10/T17 observations, P1 relative\n'
