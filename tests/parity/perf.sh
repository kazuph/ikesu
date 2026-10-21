#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
mkdir -p "$root/src/look-baseline-b0a18b0"
cd "$root/src/look-baseline-b0a18b0"
tar xf "$root/work/look-baseline-b0a18b0.tar"
go build -o "$root/work/gtx-look-baseline-b0a18b0" ./cmd/gtx
cd "$root/src/look"
go build -o "$root/work/gtx-parity-perf" ./cmd/ikesu
IKESU_PERF_BIN="$root/work/gtx-parity-perf" IKESU_PERF_BASELINE_BIN="$root/work/gtx-look-baseline-b0a18b0" IKESU_PERF_OUT="$root/work/parity-menu-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
