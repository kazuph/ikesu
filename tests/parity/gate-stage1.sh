#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
evidence=$1
mkdir -p "$evidence"
result=0
gofmt -l . > "$evidence/gofmt.log"
if [ -s "$evidence/gofmt.log" ]; then
    printf 'gofmt FAIL\n'
    result=1
else
    printf 'gofmt PASS\n'
fi
check() {
    label=$1
    shift
    if "$@" > "$evidence/$label.log" 2>&1; then
        printf '%s PASS\n' "$label"
    else
        code=$?
        printf '%s FAIL exit=%s\n' "$label" "$code"
        result=1
    fi
}
check vet go vet ./...
check vt-vet go vet github.com/charmbracelet/x/vt
check vt-test go test -count=1 github.com/charmbracelet/x/vt
check vt-race go test -race -count=1 github.com/charmbracelet/x/vt
check test go test -count=1 ./...
check race go test -race -count=1 ./...
exit "$result"
