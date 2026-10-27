#!/bin/sh
# 比べる版は cmd/ikesu を持つ版（改名後）。
set -eu
root="$HOME/bench-terrace"
. "$root/env.sh"
out="$root/work/stage7-perf"
mkdir -p "$out"
go version > "$out/build.txt"
cd "$root/work/stage7-baseline-70579fe"
go build -o "$out/baseline" ./cmd/ikesu
cd "$root/work/stage7-src"
go build -o "$out/candidate" ./cmd/ikesu
sha256sum "$out/baseline" "$out/candidate" >> "$out/build.txt"
IKESU_PERF_BIN="$out/candidate" IKESU_PERF_BASELINE_BIN="$out/baseline" IKESU_PERF_OUT="$out/relative.md" go test -count=1 -tags perf ./tests/perf -run '^TestRelative$' -timeout 30m
