#!/bin/sh
set -eu
root="$HOME/bench-terrace"
. "$root/env.sh"
out="$root/work/stage7-perf"
mkdir -p "$out"
go version > "$out/build.txt"
cd "$root/work/stage7-baseline-dd2168c"
go build -o "$out/baseline" ./cmd/gtx
cd "$root/work/stage7-src"
go build -o "$out/candidate" ./cmd/gtx
sha256sum "$out/baseline" "$out/candidate" >> "$out/build.txt"
GTX_PERF_BIN="$out/candidate" GTX_PERF_BASELINE_BIN="$out/baseline" GTX_PERF_OUT="$out/relative.md" go test -count=1 -tags perf ./tests/perf -run '^TestRelative$' -timeout 30m
