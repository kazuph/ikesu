#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
root="$HOME/bench-terrace.noindex/work/detail"
out=$1
shift
mkdir -p "$out"
go test ./internal/render ./internal/tui -run TestStage4Collapsed -count=1 > "$out/collapsed-test.log" 2>&1
go build -o "$root/gtx-stage4" ./cmd/gtx
for name in "$@"; do
    config="$root/user-config.toml"
    steps="tests/parity/stage4_cases/$name.steps"
    case "$name" in
        custom-pane|custom-workspace|custom-dim)
            config="$out/$name.toml"
            cp "$root/user-config.toml" "$config"
            case "$name" in
                custom-pane) rows='[["pane"], ["$absent"]]' ;;
                custom-workspace) rows='[["workspace"], ["$absent"]]' ;;
                custom-dim) rows='[[{token="agent", dim=true}], [{token="agent", dim=false}]]' ;;
            esac
            printf '\n[ui.sidebar.agents]\nrows = %s\n' "$rows" >> "$config"
            steps=tests/parity/stage4_cases/custom-agent.steps
            ;;
    esac
    python3 tests/parity/observe.py --herdr "$root/herdr-0.2.57" \
        --gtx "$root/gtx-stage4" --ptyrun "$root/ptyrun" \
        --config-file "$config" --server --clipboard-sandbox \
        --steps "$steps" --out "$out/$name"
done
