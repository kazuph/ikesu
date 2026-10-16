#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
cd "$root/src/stage2-baseline-a68c878"
go build -o "$root/work/detail/stage2-baseline-a68c878" ./cmd/gtx
cd "$root/src/detail"
go build -o "$root/work/detail/gtx-stage2-final" ./cmd/gtx
GTX_PERF_BIN="$root/work/detail/gtx-stage2-final" GTX_PERF_BASELINE_BIN="$root/work/detail/stage2-baseline-a68c878" GTX_PERF_OUT="$root/work/detail/stage2/perf-relative.md" go test -count=1 -tags perf ./tests/perf -run '^TestRelative$' -timeout 30m
