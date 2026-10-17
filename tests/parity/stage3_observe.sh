#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex/work/detail"
out=$1
shift
mkdir -p "$out"
go build -o "$root/gtx-stage3" ./cmd/gtx
for name in "$@"; do
    mode='--server --server-reuse-initial'
    config="$root/user-config.toml"
    toast=
    steps="tests/parity/$name.steps"
    case "$name" in
        stage3-wheel-local) mode=; steps=tests/parity/stage3-wheel.steps ;;
        stage3-toast)
            toast=--in-app-toast
            config="$out/toast.toml"
            python3 - "$root/user-config.toml" "$config" <<'PY'
import sys, tomllib
from pathlib import Path
text = Path(sys.argv[1]).read_text()
assert tomllib.loads(text)['ui']['toast'] == {'delivery': 'system'}
section = '[ui.toast]\ndelivery = "system"\n'
assert section in text
Path(sys.argv[2]).write_text(text.replace(section, '', 1))
PY
            ;;
        stage3-modifier)
            config="$out/modifier.toml"
            python3 - "$root/user-config.toml" "$config" <<'PY'
import sys
from pathlib import Path
text = Path(sys.argv[1]).read_text()
assert '[ui]\n' in text
assert 'right_click_passthrough_modifier' not in text
Path(sys.argv[2]).write_text(text.replace('[ui]\n', '[ui]\nright_click_passthrough_modifier = "ctrl"\n', 1))
PY
            ;;
    esac
    python3 tests/parity/observe.py --herdr "$root/herdr-0.2.57" \
        --gtx "$root/gtx-stage3" --ptyrun "$root/ptyrun" \
        --config-file "$config" --silent $mode $toast --clipboard-sandbox \
        --steps "$steps" --out "$out/$name"
done
