#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
out="$1"; mkdir -p "$out"
test -z "$(gofmt -l cmd internal tools tests)"
go vet ./... > "$out/vet.log" 2>&1
go test -count=1 ./... > "$out/test.log" 2>&1
go test -race -count=1 ./... > "$out/race.log" 2>&1
printf 'PASS gofmt vet test race\n'
