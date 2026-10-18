#!/bin/bash
# Both source trees live below the worker's isolated ubuntu work directory.
set -euo pipefail
source "$HOME/bench-terrace/env.sh"
work="$HOME/bench-terrace/work/tb-optimize/${1:-p1-t17}"
cd "$work"
mkdir -p p1-relative
go version > p1-relative/environment.log
go env GOOS GOARCH >> p1-relative/environment.log
uptime >> p1-relative/environment.log
for tree in baseline current; do
    (
        cd "$tree"
        go build -o "$work/p1-relative/gtx-$tree" ./cmd/gtx
    )
done
sha256sum p1-relative/gtx-baseline p1-relative/gtx-current > p1-relative/binaries.sha256
cd current
GTX_PERF_BIN="$work/p1-relative/gtx-current" \
GTX_PERF_BASELINE_BIN="$work/p1-relative/gtx-baseline" \
GTX_PERF_OUT="$work/p1-relative/relative-perf.md" \
go test -tags perf -count=1 -timeout=30m -run '^TestRelative$' -v ./tests/perf > "$work/p1-relative/perf.log" 2>&1
printf 'PASS: P1 relative on linux/amd64; no mini absolute verdict\n'
