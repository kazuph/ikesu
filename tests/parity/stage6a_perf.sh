#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
evidence="$root/work/detail/stage6a-final-checks"
mkdir -p "$evidence"
cd "$root/src/stage6a-baseline-dd2168c"
go build -o "$evidence/gtx-baseline-dd2168c" ./cmd/gtx
cd "$root/src/stage6a"
go build -o "$evidence/gtx-candidate" ./cmd/gtx
go version > "$evidence/perf-toolchain.txt"
shasum -a 256 "$evidence/gtx-baseline-dd2168c" "$evidence/gtx-candidate" > "$evidence/perf-binaries.txt"
GTX_PERF_BIN="$evidence/gtx-candidate" GTX_PERF_BASELINE_BIN="$evidence/gtx-baseline-dd2168c" GTX_PERF_OUT="$evidence/perf-relative.md" go test -count=1 -tags perf ./tests/perf -run '^TestRelative$' -timeout 30m
