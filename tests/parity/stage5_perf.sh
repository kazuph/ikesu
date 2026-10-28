#!/bin/sh
# 比べる版は cmd/ikesu を持つ版（改名後）。
set -eu
root="$1"
cd "$root/baseline"
go version
go build -o "$root/ikesu-baseline-70579fe" ./cmd/ikesu
cd "$root/candidate"
go build -o "$root/ikesu-stage5" ./cmd/ikesu
export IKESU_PERF_BIN="$root/ikesu-stage5" IKESU_PERF_BASELINE_BIN="$root/ikesu-baseline-70579fe" IKESU_PERF_OUT="$root/perf-relative.md"
python3 "$HOME/bench-terrace/work/perf-lock.py" go test -count=1 -tags perf ./tests/perf -run '^TestRelative$' -timeout 30m
