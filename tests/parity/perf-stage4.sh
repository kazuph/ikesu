#!/bin/sh
# 比べる版は cmd/ikesu を持つ版（改名後）。
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex"
out=$1
mkdir -p "$out" "$root/src/stage4-baseline-70579fe"
go version > "$out/toolchain.txt"
tar xf "$root/work/detail/stage4/baseline-70579fe.tar" -C "$root/src/stage4-baseline-70579fe"
(cd "$root/src/stage4-baseline-70579fe" && go build -o "$root/work/detail/stage4/ikesu-baseline-70579fe" ./cmd/ikesu)
go build -o "$root/work/detail/stage4/ikesu-perf" ./cmd/ikesu
shasum -a 256 "$root/work/detail/stage4/ikesu-baseline-70579fe" "$root/work/detail/stage4/ikesu-perf" > "$out/binaries.sha256"
IKESU_PERF_BIN="$root/work/detail/stage4/ikesu-perf" \
IKESU_PERF_BASELINE_BIN="$root/work/detail/stage4/ikesu-baseline-70579fe" \
IKESU_PERF_OUT="$out/perf.md" \
go test -count=1 -tags perf ./tests/perf -run '^TestRelative$' -timeout 30m > "$out/perf.log" 2>&1
