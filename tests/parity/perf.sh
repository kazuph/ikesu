#!/bin/sh
# 比べる版は cmd/ikesu を持つ版（改名後）。
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
mkdir -p "$root/src/look-baseline-70579fe"
cd "$root/src/look-baseline-70579fe"
tar xf "$root/work/look-baseline-70579fe.tar"
go build -o "$root/work/ikesu-look-baseline-70579fe" ./cmd/ikesu
cd "$root/src/look"
go build -o "$root/work/ikesu-parity-perf" ./cmd/ikesu
IKESU_PERF_BIN="$root/work/ikesu-parity-perf" IKESU_PERF_BASELINE_BIN="$root/work/ikesu-look-baseline-70579fe" IKESU_PERF_OUT="$root/work/parity-menu-perf.md" go test -count=1 -tags perf ./tests/perf -run TestRelative -timeout 30m
