#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
evidence="$root/work/detail/stage6a-perf"
mkdir -p "$evidence"
cd "$root/src/stage6a-baseline-dd2168c"
go build -o "$evidence/gtx-baseline-dd2168c" ./cmd/gtx
cd "$root/src/stage6a"
go build -o "$evidence/gtx-candidate" ./cmd/ikesu
go version > "$evidence/perf-toolchain.txt"
shasum -a 256 "$evidence/gtx-baseline-dd2168c" "$evidence/gtx-candidate" > "$evidence/perf-binaries.txt"
IKESU_PERF_BIN="$evidence/gtx-candidate" IKESU_PERF_BASELINE_BIN="$evidence/gtx-baseline-dd2168c" IKESU_PERF_OUT="$evidence/perf-relative.md" go test -count=1 -tags perf ./tests/perf -run '^TestRelative$' -timeout 30m
