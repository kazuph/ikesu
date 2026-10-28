#!/bin/sh
# 比べる版は cmd/ikesu を持つ版（改名後）。
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
evidence="$root/work/detail/stage6a-perf"
mkdir -p "$evidence"
cd "$root/src/stage6a-baseline-70579fe"
go build -o "$evidence/ikesu-baseline-70579fe" ./cmd/ikesu
cd "$root/src/stage6a"
go build -o "$evidence/ikesu-candidate" ./cmd/ikesu
go version > "$evidence/perf-toolchain.txt"
shasum -a 256 "$evidence/ikesu-baseline-70579fe" "$evidence/ikesu-candidate" > "$evidence/perf-binaries.txt"
IKESU_PERF_BIN="$evidence/ikesu-candidate" IKESU_PERF_BASELINE_BIN="$evidence/ikesu-baseline-70579fe" IKESU_PERF_OUT="$evidence/perf-relative.md" go test -count=1 -tags perf ./tests/perf -run '^TestRelative$' -timeout 30m
