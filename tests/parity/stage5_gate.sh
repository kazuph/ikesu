#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
out="$1"; mkdir -p "$out"
go version > "$out/go-version.txt"
python3 - "$out/source-sha256.json" <<'PYHASH'
import hashlib,json,sys
from pathlib import Path
files=[p for folder in ('cmd','internal','tools','tests') for p in Path(folder).rglob('*.go')]+[Path('go.mod'),Path('go.sum')]
Path(sys.argv[1]).write_text(json.dumps({str(p):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(files)},indent=2)+'\n')
PYHASH
gofmt -l cmd internal tools tests > "$out/gofmt.log"
test ! -s "$out/gofmt.log"
go vet ./... > "$out/vet.log" 2>&1
go test -count=1 ./... > "$out/test.log" 2>&1
go test -race -count=1 ./... > "$out/race.log" 2>&1
printf 'PASS gofmt vet test race\n'
