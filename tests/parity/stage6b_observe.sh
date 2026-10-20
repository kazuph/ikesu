#!/bin/sh
set -eu
out=$1
shift
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
work="$HOME/bench-terrace.noindex/work/detail"
go build -o "$work/gtx-stage6b" ./cmd/ikesu
python3 tests/parity/stage6b_cases.py tests/parity/stage6b_cases
config="$out/config.toml"
mkdir -p "$out"
python3 - "$work/user-config.toml" "$config" <<'PY'
import sys
from pathlib import Path
text = Path(sys.argv[1]).read_text().replace('[keys]\n', '[keys]\ngoto="prefix+o"\n')
Path(sys.argv[2]).write_text(text+'\n[ui.sound]\nenabled=false\n')
PY
if [ "$#" = 0 ]; then
  set -- tests/parity/stage6b_cases/*.steps
fi
for procedure in "$@"; do
  name=$(basename "$procedure" .steps)
  selected="$config"
  case "$name" in
    k22-user) sed '/goto="prefix+o"/d' "$config" > "$out/user.toml"; selected="$out/user.toml" ;;
    k22-nav) sed '/\[keys\]/a\
navigate_workspace_up="u"\
navigate_workspace_down="d"\
navigate_pane_left="a"\
navigate_pane_down="s"\
navigate_pane_up="w"\
navigate_pane_right="f"\
' "$config" > "$out/nav.toml"; selected="$out/nav.toml" ;;
  esac
  python3 tests/parity/observe.py --herdr "$work/herdr-0.2.57" --ikesu "$work/gtx-stage6b" --ptyrun "$work/ptyrun" --config-file "$selected" --steps "$procedure" --out "$out/$name" --silent --clipboard-sandbox --server
done
