#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
out=$1
mkdir -p "$out"
result=0
gofmt -l . > "$out/gofmt.log"
if [ -s "$out/gofmt.log" ]; then
    printf 'gofmt FAIL\n'
    result=1
else
    printf 'gofmt PASS\n'
fi
check() {
    label=$1
    shift
    if "$@" > "$out/$label.log" 2>&1; then
        printf '%s PASS\n' "$label"
    else
        code=$?
        printf '%s FAIL exit=%s\n' "$label" "$code"
        result=1
    fi
}
check vet go vet ./...
check test go test -count=1 ./...
check race go test -race -count=1 ./...
exit "$result"
