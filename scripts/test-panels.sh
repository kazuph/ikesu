#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
# Existing native helpers use a 60s subprocess deadline; the longest measured
# complete test is 35.6s. Bound every test, including its file, to the same 60s.
# A panel in front takes the keyboard (2026-11-09), so tests that check which
# panel has the keyboard run one at a time, after the others.
serial="apps/mac-dialog-decisions/test/body-links.test.mjs apps/mac-dialog-decisions/test/activation.test.mjs apps/mac-dialog-decisions/test/display-native.test.mjs apps/mac-dialog-decisions/test/panel-front.test.mjs"
set --
for test_file in apps/mac-dialog-decisions/test/*.test.mjs apps/slack-decisions/test/*.test.mjs; do
  case " $serial " in *" $test_file "*) ;; *) set -- "$@" "$test_file" ;; esac
done
status=0
node --test --test-timeout=60000 --test-concurrency=2 "$@" || status=1
# shellcheck disable=SC2086
node --test --test-timeout=60000 --test-concurrency=1 $serial || status=1
exit "$status"
