# Bench environment on mini: the same Go/MoonBit toolchains as the main Mac, isolated under ~/bench-terrace.
B=$HOME/bench-terrace.noindex
W=$B/work
mkdir -p $W/logs
export MOON_HOME=$B/tools/moon
export PATH=$B/tools/go/bin:$MOON_HOME/bin:$HOME/.cargo/bin:$HOME/.local/share/mise/installs/zig/0.15.2/bin:/opt/homebrew/bin:$PATH
export DEVELOPER_DIR=/Library/Developer/CommandLineTools
export GOTOOLCHAIN=local
export GOMODCACHE=$B/gomodcache
export CARGO_HOME=$HOME/.cargo
now(){ python3 -c 'import time;print(time.time())'; }
tm(){ local n=$1; shift; local s; s=$(now); "$@" >"$W/logs/$n.log" 2>&1; local rc=$?; python3 -c "import time;print('$n', round(time.time()-$s,1), 'rc=$rc')"; }
