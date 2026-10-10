#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
candidate="$1"
report="$2"
cd "$root/src/look-baseline-b0a18b0"
go build -o "$root/work/gtx-look-baseline-b0a18b0" ./cmd/gtx
cd "$root/src/look"
GTX_PERF_BIN="$root/work/$candidate" GTX_PERF_BASELINE_BIN="$root/work/gtx-look-baseline-b0a18b0" GTX_PERF_OUT="$root/work/$report" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
