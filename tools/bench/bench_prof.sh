#!/bin/bash
# CPU profile of ikesu while it shows 2,000,000 lines (same 160x45 PTY as bench_rt.sh).
# Usage: bench_prof.sh <label> [profile seconds]
set -u
. $HOME/bench-terrace.noindex/env.sh
R=$W/rt; mkdir -p $R
label=${1:-prof}; secs=${2:-40}
rm -f $R/$label.elapsed $R/$label.pprof
cat > $R/$label.steps <<STEPS
sleep 5s
send "python3 $R/tp.py $R/$label.elapsed\r"
waitfile $R/$label.elapsed 900s
sleep 2s
stats $label
kill
STEPS
echo "load=$(uptime | sed 's/.*averages: //')"
s=$(now)
(cd ~ && IKESU_TRACE=${IKESU_TRACE:-} IKESU_CPUPROFILE=$R/$label.pprof IKESU_CPUPROFILE_SECS=$secs $W/ptyrun -cols 160 -rows 45 -steps $R/$label.steps -- ${IKESU_BIN:-$W/ikesu-prof})
echo "$label elapsed=$(cat $R/$label.elapsed 2>/dev/null) wall_total=$(python3 -c "import time;print(round(time.time()-$s,1))")"
