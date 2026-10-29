#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
# Existing native helpers use a 60s subprocess deadline; the longest measured
# complete test is 35.6s. Bound every test, including its file, to the same 60s.
exec node --test --test-timeout=60000 --test-concurrency=2 \
  apps/mac-dialog-decisions/test/*.test.mjs \
  apps/slack-decisions/test/*.test.mjs
