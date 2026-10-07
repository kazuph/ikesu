#!/bin/bash
# Time from Ctrl-C during the 2,000,000-line flood until the interrupt shows on
# screen (how much already-read output the multiplexer replays first).
# Usage: bench_interrupt.sh <label> <command...>
set -u
. $HOME/bench-terrace.noindex/env.sh
R=$W/rt; mkdir -p $R
label=$1; shift
cat > $R/$label.steps <<STEPS
sleep 5s
send "python3 $R/tp.py $R/$label.elapsed\r"
sleep 1s
send "\x03"
waittext 60s KeyboardInterrupt
kill
STEPS
out=$(cd ~ && $W/ptyrun -cols 160 -rows 45 -steps $R/$label.steps -- "$@")
echo "$label load=$(uptime | sed 's/.*averages: //') ctrl-c $(echo "$out" | grep onscreen)"
