#!/bin/sh
set -eu
root=$1
source=$2
export PATH="$root/tools/go/bin:$PATH"
out="$root/work/detail/stage6b-perf"
base="$root/work/stage6b-baseline-4d337f7"
mkdir -p "$out" "$base"
tar xf "$out/baseline.tar" -C "$base"
go version > "$out/build.txt"
go env GOOS GOARCH >> "$out/build.txt"
cd "$base"
go build -o "$out/baseline" ./cmd/gtx
cd "$source"
go build -o "$out/candidate" ./cmd/gtx
shasum -a 256 "$out/baseline" "$out/candidate" >> "$out/build.txt"
GTX_PERF_BIN="$out/candidate" GTX_PERF_BASELINE_BIN="$out/baseline" GTX_PERF_OUT="$out/relative.md" go test -count=1 -tags perf ./tests/perf -run '^TestRelative$' -timeout 30m
