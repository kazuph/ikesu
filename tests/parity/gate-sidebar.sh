#!/bin/sh
# gtx's independent left/right layout intentionally differs from herdr.
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
out="$HOME/bench-terrace.noindex/work/right-sidebar/screens"
mkdir -p "$out"
GTX_SIDEBAR_EVIDENCE="$out" go test -count=1 ./tests/e2e -run '^TestRightSidebar' -timeout 5m
python3 tests/parity/right_sidebar_gate.py "$out"
