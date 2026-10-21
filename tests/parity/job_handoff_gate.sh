#!/bin/sh
# Run with the host's Go 1.27 environment already sourced; all state is isolated.
set -eu
mode=$1
archive=$2
out=$3
mkdir -p "$out/baseline-source"
out=$(cd "$out" && pwd)
tar -xf "$archive" -C "$out/baseline-source"
{
    hostname
    go version
    go env GOOS GOARCH
    uptime
    printf '%s\n' 'baseline=2e01183' 'build flags=-trimpath -ldflags=-s -w'
} > "$out/conditions.log"
(cd "$out/baseline-source" && go build -trimpath '-ldflags=-s -w' -o "$out/baseline" ./cmd/gtx)
export IKESU_JOB_HANDOFF_OLD_BIN="$out/baseline"
failed=0
check() {
    name=$1
    shift
    printf '%s\n' "$name: $*" >> "$out/commands.log"
    if "$@" > "$out/$name.log" 2>&1; then
        printf '%s\n' "$name PASS" >> "$out/summary.log"
    else
        printf '%s\n' "$name FAIL" >> "$out/summary.log"
        failed=1
    fi
}
if [ "$mode" = ubuntu ]; then
    # Format only the paths changed by this task, using Go 1.27's formatter.
    gofmt -l cmd/ikesu/main.go internal/server/job_cache.go internal/server/jobs.go \
        internal/server/jobs_process_darwin.go internal/server/jobs_process_linux.go \
        internal/server/main_test.go internal/server/server.go internal/server/handoff.go \
        internal/server/mail.go internal/server/job_runner.go internal/server/job_runner_test.go \
        internal/server/jobs_log.go internal/dispatch/job_completion.go \
        internal/dispatch/job_completion_test.go internal/dispatch/job_cleanup.go \
        internal/dispatch/job_cleanup_test.go tests/e2e/job_handoff_test.go \
        internal/server/job_monitor.go internal/server/job_monitor_lifecycle_test.go \
        tests/e2e/job_handoff_fault_test.go > "$out/gofmt.log"
    if [ -s "$out/gofmt.log" ]; then printf '%s\n' 'gofmt FAIL' >> "$out/summary.log"; failed=1
    else printf '%s\n' 'gofmt PASS' >> "$out/summary.log"; fi
    check vet go vet ./...
fi
check all go test -json -count=1 -timeout=30m ./...
check race go test -race -json -count=1 -timeout=30m ./...
if [ "$mode" = mini ]; then
    go build -trimpath '-ldflags=-s -w' -o "$out/candidate" ./cmd/ikesu
    shasum -a 256 "$out/baseline" "$out/candidate" > "$out/binaries.sha256"
    export IKESU_PERF_BIN="$out/candidate" IKESU_PERF_BASELINE_BIN="$out/baseline" IKESU_PERF_OUT="$out/relative.md"
    check relative go test -tags perf -count=1 -timeout=30m -run '^TestRelative$' -v ./tests/perf
fi
cat "$out/summary.log"
exit "$failed"
