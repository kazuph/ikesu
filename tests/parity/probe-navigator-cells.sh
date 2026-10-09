#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
cd "$HOME/bench-terrace.noindex/src/parity"
work="$HOME/bench-terrace.noindex/work"
(cd tools/bench/ptyrun && gofmt -w main.go && go build -o "$work/ptyrun-parity-cells" .)
python3 tests/parity/observe.py --herdr "$work/herdr-target/release/herdr" --gtx "$work/gtx-parity-input-final" --ptyrun "$work/ptyrun-parity-cells" --steps tests/parity/navigate-detail.steps --out "$work/parity-navigate-cells"
