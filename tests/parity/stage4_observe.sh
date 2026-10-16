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
        git-default)
            steps=tests/parity/stage4_cases/custom-git.steps
            ;;
        job-cancelling)
            steps=tests/parity/jobs-cancelling.steps
            ;;
        custom-git)
            config="$out/$name.toml"
            cp "$root/user-config.toml" "$config"
            printf '\n[ui.sidebar.spaces]\nrows = [["git_status"], ["git_diff"]]\n' >> "$config"
            ;;
        custom-pane|custom-workspace|custom-dim|custom-focus)
            config="$out/$name.toml"
            cp "$root/user-config.toml" "$config"
            case "$name" in
                custom-pane) rows='[["pane"], ["$absent"]]' ;;
                custom-workspace) rows='[["workspace"], ["$absent"]]' ;;
                custom-dim) rows='[[{token="agent", dim=true}], [{token="agent", dim=false}]]' ;;
                custom-focus) rows='[["workspace"], ["agent"]]' ;;
            esac
            printf '\n[ui.sidebar.agents]\nrows = %s\n' "$rows" >> "$config"
            steps=tests/parity/stage4_cases/custom-agent.steps
            if [ "$name" = custom-focus ]; then
                steps=tests/parity/stage4_cases/focus-agents.steps
            fi
            ;;
    esac
    python3 tests/parity/observe.py --herdr "$root/herdr-0.2.57" \
        --gtx "$root/gtx-stage4" --ptyrun "$root/ptyrun" \
        --config-file "$config" --silent --server --clipboard-sandbox \
        --steps "$steps" --out "$out/$name"
done
