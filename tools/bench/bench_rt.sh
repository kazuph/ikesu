#!/bin/bash
# Throughput on mini: 2,000,000 lines printed inside each multiplexer, all in the same 160x45 PTY.
set -u
. $HOME/bench-terrace.noindex/env.sh
R=$W/rt; mkdir -p $R
cat > $R/tp.py <<'EOF'
import time,sys
s=time.time()
w=sys.stdout.write
for i in range(2000000): w("%d line of throughput test\n" % i)
sys.stdout.flush()
open(sys.argv[1],"w").write("%.2f\n" % (time.time()-s))
EOF
HX=$W/hx; mkdir -p $HX/cfg $HX/state
MT=$B/src/moonterrace/_build/native
run(){ # label, command...
  local label=$1; shift
  rm -f $R/$label.elapsed
  cat > $R/$label.steps <<EOF
sleep 5s
screen $R/$label.before.txt
send "python3 $R/tp.py $R/$label.elapsed\r"
waitfile $R/$label.elapsed 900s
sleep 2s
stats $label
screen $R/$label.after.txt
kill
EOF
  local s; s=$(now)
  (cd ~ && $W/ptyrun -cols 160 -rows 45 -steps $R/$label.steps -- "$@")
  echo "$label elapsed=$(cat $R/$label.elapsed 2>/dev/null) wall_total=$(python3 -c "import time;print(round(time.time()-$s,1))")"
}
for round in 1 2 3; do
  echo "## round $round load=$(uptime | sed 's/.*averages: //')"
  run plain_r$round /bin/zsh -l
  run herdr_rel_r$round env -u HERDR_ENV -u HERDR_PANE_ID -u HERDR_ACTIVE_PANE_ID -u HERDR_SOCKET_PATH -u HERDR_CLIENT_SOCKET_PATH XDG_CONFIG_HOME=$HX/cfg XDG_STATE_HOME=$HX/state $W/herdr-target/release/herdr --no-session
  run mt_rel_r$round $MT/release/build/cmd/mt/mt.exe
  run mt_dev_r$round $MT/debug/build/cmd/mt/mt.exe
  run gtx_rel_r$round $W/gtx-rel
done
stat -f '%z %N' $W/herdr-target/release/herdr $MT/release/build/cmd/mt/mt.exe $MT/debug/build/cmd/mt/mt.exe $W/gtx-rel $W/gtx-dev
