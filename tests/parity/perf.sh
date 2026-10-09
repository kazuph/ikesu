#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
mkdir -p "$root/src/parity-baseline-9347b07"
cd "$root/src/parity-baseline-9347b07"
tar xf "$root/work/parity-baseline-9347b07.tar"
go build -o "$root/work/gtx-parity-baseline-9347b07" ./cmd/gtx
cd "$root/src/parity"
go build -o "$root/work/gtx-parity-perf" ./cmd/gtx
GTX_PERF_BIN="$root/work/gtx-parity-perf" GTX_PERF_BASELINE_BIN="$root/work/gtx-parity-baseline-9347b07" GTX_PERF_OUT="$root/work/parity-menu-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
