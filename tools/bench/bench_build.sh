#!/bin/bash
# Build/lint/test timing on mini for herdr (Rust), moonterrace (MoonBit), goterrace (Go). Sequential.
set -u
. $HOME/bench-terrace.noindex/env.sh
which="${1:-all}"
echo "# host $(hostname) $(sysctl -n machdep.cpu.brand_string) cores=$(sysctl -n hw.ncpu) load=$(uptime | sed 's/.*averages: //')"

if [ "$which" = all ] || [ "$which" = go ]; then
  echo "## goterrace ($(go version | awk '{print $3}'))"
  cd $B/src/goterrace || exit 1
  export GOCACHE=$W/gocache
  rm -rf "$GOCACHE"
  tm go_dev_clean go build -o $W/gtx-dev ./cmd/ikesu
  touch internal/render/render.go; tm go_dev_incr go build -o $W/gtx-dev ./cmd/ikesu
  touch internal/render/render.go; tm go_dev_incr2 go build -o $W/gtx-dev ./cmd/ikesu
  rm -rf "$GOCACHE"
  tm go_rel_clean go build -trimpath -ldflags=-s\ -w -o $W/gtx-rel ./cmd/ikesu
  touch internal/render/render.go; tm go_rel_incr go build -trimpath -ldflags=-s\ -w -o $W/gtx-rel ./cmd/ikesu
  touch internal/render/render.go; tm go_rel_incr2 go build -trimpath -ldflags=-s\ -w -o $W/gtx-rel ./cmd/ikesu
  touch internal/render/render.go; tm go_vet go vet ./...
  tm go_fmt_check gofmt -l .
  touch internal/render/render.go; tm go_test go test -count=1 ./...
  unset GOCACHE
fi

if [ "$which" = all ] || [ "$which" = mt ]; then
  echo "## moonterrace ($(moon version | head -1))"
  cd $B/src/moonterrace || exit 1
  moon clean >/dev/null 2>&1
  tm mt_dev_clean moon build --target native
  touch src/render/render.mbt; tm mt_dev_incr moon build --target native
  touch src/render/render.mbt; tm mt_dev_incr2 moon build --target native
  touch src/render/render.mbt; tm mt_check moon check --target native
  tm mt_fmt_check moon fmt --check
  touch src/render/render.mbt; tm mt_test moon test --target native
  moon clean >/dev/null 2>&1
  tm mt_rel_clean moon build --target native --release
  touch src/render/render.mbt; tm mt_rel_incr moon build --target native --release
  touch src/render/render.mbt; tm mt_rel_incr2 moon build --target native --release
  tm mt_dev_rebuild moon build --target native
fi

if [ "$which" = all ] || [ "$which" = herdr ]; then
  cd $B/src/herdr || exit 1
  echo "## herdr ($(rustc --version | awk '{print $2}'), commit ccb002ce)"
  export CARGO_TARGET_DIR=$W/herdr-target
  rm -rf $CARGO_TARGET_DIR
  tm rs_dev_clean cargo build
  touch src/ui/sidebar.rs; tm rs_dev_incr cargo build
  touch src/ui/sidebar.rs; tm rs_dev_incr2 cargo build
  touch src/ui/sidebar.rs; tm rs_check cargo check
  touch src/ui/sidebar.rs; tm rs_clippy cargo clippy
  tm rs_fmt_check cargo fmt --check
  tm rs_rel_clean cargo build --release
  touch src/ui/sidebar.rs; tm rs_rel_incr cargo build --release
  touch src/ui/sidebar.rs; tm rs_rel_incr2 cargo build --release
  touch src/ui/sidebar.rs; tm rs_test cargo test
fi
