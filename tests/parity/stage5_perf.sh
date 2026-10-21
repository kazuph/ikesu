#!/bin/sh
set -eu
root="$1"
cd "$root/baseline"
go version
go build -o "$root/gtx-baseline-dd2168c" ./cmd/gtx
cd "$root/candidate"
go build -o "$root/gtx-stage5" ./cmd/ikesu
export IKESU_PERF_BIN="$root/gtx-stage5" IKESU_PERF_BASELINE_BIN="$root/gtx-baseline-dd2168c" IKESU_PERF_OUT="$root/perf-relative.md"
python3 "$HOME/bench-terrace/work/perf-lock.py" go test -count=1 -tags perf ./tests/perf -run '^TestRelative$' -timeout 30m
