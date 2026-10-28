#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex/work/detail"
out="$1"
go build -o "$root/ikesu-stage5" ./cmd/ikesu
python3 tests/parity/stage5_cases.py "$out/procedures" "$root/user-config.toml"
for steps in "$out"/procedures/*.steps; do
 name=$(basename "$steps" .steps)
 extra=""
 if [ "$name" = empty ]; then extra="--close-workspace-fixture"; fi
 mode="--server --server-reuse-initial"
 if [ "$name" = narrow ]; then mode=""; fi
 python3 tests/parity/observe.py --herdr "$root/herdr-0.2.57" --ikesu "$root/ikesu-stage5" --ptyrun "$root/ptyrun" --config-file "$out/procedures/$name.toml" --steps "$steps" --out "$out/$name" --silent --clipboard-sandbox $mode $extra
done

for mode in server local; do
 extra=""
 if [ "$mode" = server ]; then extra="--server --server-default-workspace"; fi
 python3 tests/parity/observe.py --herdr "$root/herdr-0.2.57" --ikesu "$root/ikesu-stage5" --ptyrun "$root/ptyrun" --config-file "$root/user-config.toml" --steps tests/parity/stage5-empty-ui.steps --out "$out/empty-ui-$mode" --silent --clipboard-sandbox $extra
 python3 tests/parity/observe.py --herdr "$root/herdr-0.2.57" --ikesu "$root/ikesu-stage5" --ptyrun "$root/ptyrun" --config-file "$root/user-config.toml" --steps tests/parity/stage5-startup.steps --out "$out/startup-empty-$mode" --saved-session-fixture tests/parity/stage5-empty-session.json --silent --clipboard-sandbox $extra
done
