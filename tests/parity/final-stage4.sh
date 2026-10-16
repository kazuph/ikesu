#!/bin/sh
set -eu
out=$1
mkdir -p "$out/gates" "$out/observations" "$out/perf"
sh tests/parity/gate-stage4.sh "$out/gates" > "$out/gates/summary.log" 2>&1
sh tests/parity/stage4_observe.sh "$out/observations" \
    controls collapsed-agents agent-scroll custom-pane custom-workspace custom-dim \
    focus-agents custom-focus named-tab worktree-active worktree-selected panel-switch job-owner job-unknown job-minute \
    custom-git-local git-default-local custom-git git-default \
    > "$out/observations/run.log" 2>&1
python3 tests/parity/stage4_initial_gate.py "$out/observations/controls" "$out/observations/collapsed-agents" > "$out/initial-gate.json"
python3 tests/parity/stage4_cards_gate.py "$out/observations/agent-scroll" "$out/observations" > "$out/cards-gate.json"
python3 tests/parity/stage4_dim_gate.py "$out/observations/custom-dim" > "$out/dim-gate.json"
python3 tests/parity/stage4_focus_gate.py "$out/observations" > "$out/focus-gate.json"
python3 tests/parity/stage4_names_gate.py "$out/observations/named-tab" > "$out/names-gate.json"
python3 tests/parity/stage4_worktree_gate.py "$out/observations/worktree-active" > "$out/worktree-gate.json"
python3 tests/parity/stage4_worktree_gate.py "$out/observations/worktree-selected" selected > "$out/worktree-selected-gate.json"
python3 tests/parity/stage4_panel_gate.py "$out/observations/panel-switch" > "$out/panel-gate.json"
python3 tests/parity/stage4_job_gate.py "$out/observations/job-owner" > "$out/job-gate.json"
python3 tests/parity/stage4_unknown_gate.py "$out/observations/job-unknown" > "$out/unknown-gate.json"
python3 tests/parity/stage4_minute_gate.py "$out/observations/job-minute" > "$out/minute-gate.json"
python3 tests/parity/stage4_git_gate.py "$out/observations" > "$out/git-gate.json"
python3 tests/parity/stage4_silence_gate.py "$out/observations" > "$out/silence-gate.json"
python3 "$HOME/bench-terrace.noindex/work/perf-lock.py" sh tests/parity/perf-stage4.sh "$out/perf"
