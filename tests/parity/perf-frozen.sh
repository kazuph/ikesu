#!/bin/sh
# 比べる版は cmd/ikesu を持つ版（改名後）。
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
candidate="$1"
report="$2"
cd "$root/src/look-baseline-70579fe"
go build -o "$root/work/ikesu-look-baseline-70579fe" ./cmd/ikesu
cd "$root/src/look"
IKESU_PERF_BIN="$root/work/$candidate" IKESU_PERF_BASELINE_BIN="$root/work/ikesu-look-baseline-70579fe" IKESU_PERF_OUT="$root/work/$report" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
