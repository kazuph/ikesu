#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
# Existing native helpers use a 60s subprocess deadline; the longest measured
# complete test is 35.6s. Bound every test, including its file, to the same 60s.
set --
for test_file in apps/mac-dialog-decisions/test/*.test.mjs apps/slack-decisions/test/*.test.mjs; do
  [ "$test_file" = apps/mac-dialog-decisions/test/body-links.test.mjs ] || set -- "$@" "$test_file"
done
status=0
node --test --test-timeout=60000 --test-concurrency=2 "$@" || status=1
node --test --test-timeout=60000 --test-concurrency=1 apps/mac-dialog-decisions/test/body-links.test.mjs || status=1
exit "$status"
