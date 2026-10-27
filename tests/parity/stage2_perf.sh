#!/bin/sh
# 比べる版は cmd/ikesu を持つ版（改名後）。
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
cd "$root/src/stage2-baseline-70579fe"
go build -o "$root/work/detail/stage2-baseline-70579fe" ./cmd/ikesu
cd "$root/src/detail"
go build -o "$root/work/detail/ikesu-stage2-final" ./cmd/ikesu
IKESU_PERF_BIN="$root/work/detail/ikesu-stage2-final" IKESU_PERF_BASELINE_BIN="$root/work/detail/stage2-baseline-70579fe" IKESU_PERF_OUT="$root/work/detail/stage2/perf-relative.md" go test -count=1 -tags perf ./tests/perf -run '^TestRelative$' -timeout 30m
