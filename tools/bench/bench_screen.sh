#!/bin/bash
# Time from sending the 2,000,000-line command until its last line is visible
# on the multiplexer's screen (python's own elapsed only shows when the
# producer finished writing, which a large input queue can hide).
# Usage: bench_screen.sh <label> <command...>
set -u
. $HOME/bench-terrace.noindex/env.sh
R=$W/rt; mkdir -p $R
label=$1; shift
rm -f $R/$label.elapsed
cat > $R/$label.steps <<STEPS
sleep 5s
send "python3 $R/tp.py $R/$label.elapsed\r"
waittext 300s 1999999 line of throughput test
waitfile $R/$label.elapsed 300s
stats $label
kill
STEPS
out=$(cd ~ && $W/ptyrun -cols 160 -rows 45 -steps $R/$label.steps -- "$@")
echo "$label load=$(uptime | sed 's/.*averages: //') python_elapsed=$(cat $R/$label.elapsed 2>/dev/null) $(echo "$out" | grep onscreen) $(echo "$out" | grep -o 'rss_kb,cpu=[^o]*')"
