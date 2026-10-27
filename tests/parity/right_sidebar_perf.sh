#!/bin/sh
# 比べる版は cmd/ikesu を持つ版（改名後）。
set -eu
candidate="$1"
archive="$2"
out="$3"
mkdir -p "$out/baseline-source"
tar -xf "$archive" -C "$out/baseline-source"
{
 hostname
 go version
 printf '%s\n' 'baseline=70579fe' 'flags=go build -trimpath -ldflags=-s -w'
} > "$out/build-conditions.log"
(cd "$out/baseline-source" && go build -trimpath '-ldflags=-s -w' -o "$out/baseline" ./cmd/ikesu)
(cd "$candidate" && go build -trimpath '-ldflags=-s -w' -o "$out/candidate" ./cmd/ikesu)
go version -m "$out/baseline" >> "$out/build-conditions.log"
go version -m "$out/candidate" >> "$out/build-conditions.log"
python3 - "$out" <<'PY'
from pathlib import Path
import hashlib,json,sys
p=Path(sys.argv[1])
(p/'binary-sha256.json').write_text(json.dumps({n:hashlib.sha256((p/n).read_bytes()).hexdigest() for n in ('baseline','candidate')},indent=2)+'\n')
PY
cd "$candidate"
IKESU_PERF_BIN="$out/candidate" IKESU_PERF_BASELINE_BIN="$out/baseline" IKESU_PERF_OUT="$out/relative.md" go test -tags perf -count=1 -timeout 30m -run '^TestRelative$' -v ./tests/perf > "$out/relative.log" 2>&1
